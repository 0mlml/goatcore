package onboardstore

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

// realEd25519Key is a genuine, well-formed public key GENERATED AT TEST TIME.
//
// Deliberately not a literal: ops/ci/check-no-identity-spillage.sh refuses SSH
// pubkeys in committed files, and that guard is right — exempting it to park a
// convenient fixture would weaken a control that exists to keep operator
// identity material out of the public surface. Generating the key is also a
// stronger test: it exercises the real OpenSSH wire format
// (string(type) || string(rawkey), each length-prefixed) rather than a blob
// someone pasted once and nobody can re-derive.
var realEd25519Key = mustGenerateEd25519AuthorizedKey("person@laptop")

func mustGenerateEd25519AuthorizedKey(comment string) string {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic("generate ed25519 test key: " + err.Error())
	}
	var wire []byte
	appendString := func(b []byte) {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(b)))
		wire = append(wire, l[:]...)
		wire = append(wire, b...)
	}
	appendString([]byte("ssh-ed25519"))
	appendString(pub)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(wire) + " " + comment
}

// The parser is the security boundary of this feature. A key that gets past it
// is a line in authorized_keys on a production host, so the adversarial cases
// matter more than the happy path.
func TestParseSSHPublicKey(t *testing.T) {
	t.Run("accepts a real key and splits it", func(t *testing.T) {
		kt, body, comment, err := ParseSSHPublicKey(realEd25519Key)
		if err != nil {
			t.Fatalf("valid key rejected: %v", err)
		}
		if kt != "ssh-ed25519" || comment != "person@laptop" || body == "" {
			t.Errorf("bad split: type=%q body=%q comment=%q", kt, body, comment)
		}
	})

	t.Run("accepts a key with no comment", func(t *testing.T) {
		f := strings.Fields(realEd25519Key)
		if _, _, c, err := ParseSSHPublicKey(f[0] + " " + f[1]); err != nil || c != "" {
			t.Errorf("comment is optional: comment=%q err=%v", c, err)
		}
	})

	t.Run("accepts a comment containing spaces", func(t *testing.T) {
		f := strings.Fields(realEd25519Key)
		_, _, c, err := ParseSSHPublicKey(f[0] + " " + f[1] + " my laptop 2026")
		if err != nil || c != "my laptop 2026" {
			t.Errorf("multi-word comment: comment=%q err=%v", c, err)
		}
	})

	// The one input that must never be stored: someone pastes their PRIVATE key.
	// Storing it would put a live credential in the console DB and in any audit
	// log that echoed the submission.
	t.Run("refuses a private key", func(t *testing.T) {
		_, _, _, err := ParseSSHPublicKey("-----BEGIN OPENSSH PRIVATE KEY----- abc")
		if !errors.Is(err, ErrSSHKeyBadFormat) || !strings.Contains(err.Error(), "PRIVATE") {
			t.Errorf("private key must be refused by name, got %v", err)
		}
	})

	// Type-spoofing: a well-formed ed25519 body relabelled as ssh-rsa. Accepting
	// it would write a line sshd rejects, and the person would believe they had
	// access they do not have.
	t.Run("refuses a declared type that disagrees with the body", func(t *testing.T) {
		f := strings.Fields(realEd25519Key)
		if _, _, _, err := ParseSSHPublicKey("ssh-rsa " + f[1]); !errors.Is(err, ErrSSHKeyBadFormat) {
			t.Errorf("type/body mismatch must be refused, got %v", err)
		}
	})

	t.Run("refuses unsupported and weak types", func(t *testing.T) {
		f := strings.Fields(realEd25519Key)
		for _, kt := range []string{"ssh-dss", "ssh-unknown", "rsa"} {
			if _, _, _, err := ParseSSHPublicKey(kt + " " + f[1]); !errors.Is(err, ErrSSHKeyBadFormat) {
				t.Errorf("%s must be refused, got %v", kt, err)
			}
		}
	})

	t.Run("refuses mangled and empty input", func(t *testing.T) {
		for name, in := range map[string]string{
			"empty":        "",
			"type only":    "ssh-ed25519",
			"not base64":   "ssh-ed25519 !!!!not-base64!!!!",
			"line wrapped": "ssh-ed25519 AAAAC3NzaC1lZDI1\nNTE5AAAA rest",
			"short body":   "ssh-ed25519 " + base64.StdEncoding.EncodeToString([]byte{1, 2}),
		} {
			if _, _, _, err := ParseSSHPublicKey(in); !errors.Is(err, ErrSSHKeyBadFormat) {
				t.Errorf("%s must be refused, got %v", name, err)
			}
		}
	})

	t.Run("fingerprint matches the OpenSSH SHA256 form sshd logs", func(t *testing.T) {
		fp := SSHKeyFingerprint(strings.Fields(realEd25519Key)[1])
		if !strings.HasPrefix(fp, "SHA256:") || strings.HasSuffix(fp, "=") {
			t.Errorf("fingerprint %q must be SHA256: base64 with padding stripped", fp)
		}
	})
}

// The gate is the reason this feature is safe to expose. These tests assert
// that a submission grants nothing and that no ordering of calls reaches
// `installed` without an explicit approval.
func TestSSHKeyGate(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	f := strings.Fields(realEd25519Key)

	submit := func(handle string) *SSHKey {
		t.Helper()
		k, err := st.SubmitSSHKey(ctx, &SSHKey{
			Handle: handle, KeyType: f[0], KeyBody: f[1], Comment: f[2], Env: "zagros-prod",
		}, 90)
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		return k
	}

	t.Run("a submission grants nothing", func(t *testing.T) {
		k := submit("alice")
		if k.Status != SSHKeySubmitted {
			t.Fatalf("new submission status = %q, want %q", k.Status, SSHKeySubmitted)
		}
		// The gate: install must be unreachable until an admin approves.
		if err := st.MarkSSHKeyInstalled(ctx, k.ID, "installer"); !errors.Is(err, ErrSSHKeyWrongState) {
			t.Errorf("install before approve must refuse, got %v", err)
		}
		got, _ := st.SSHKeyByID(ctx, k.ID)
		if got.Status != SSHKeySubmitted {
			t.Errorf("refused install changed state to %q", got.Status)
		}
	})

	t.Run("approve then install is the only path", func(t *testing.T) {
		k := submit("bob")
		if err := st.ApproveSSHKey(ctx, k.ID, "admin@x"); err != nil {
			t.Fatalf("approve: %v", err)
		}
		if got, _ := st.SSHKeyByID(ctx, k.ID); got.Status != SSHKeyApproved || got.DecidedBy != "admin@x" {
			t.Errorf("approve did not record decider: %+v", got)
		}
		if err := st.MarkSSHKeyInstalled(ctx, k.ID, "installer"); err != nil {
			t.Fatalf("install after approve: %v", err)
		}
		got, _ := st.SSHKeyByID(ctx, k.ID)
		if got.Status != SSHKeyInstalled || got.InstalledAt == 0 {
			t.Errorf("install did not stamp: %+v", got)
		}
		// Approving twice must not silently re-open a decided row.
		if err := st.ApproveSSHKey(ctx, k.ID, "admin@x"); !errors.Is(err, ErrSSHKeyWrongState) {
			t.Errorf("re-approve must refuse, got %v", err)
		}
	})

	// Only the SUBMITTER reopens a rejected key (by resubmitting it, below);
	// no admin transition starts from `rejected`.
	t.Run("admin transitions cannot resurrect a rejected key", func(t *testing.T) {
		k := submit("carol")
		if err := st.RejectSSHKey(ctx, k.ID, "admin@x"); err != nil {
			t.Fatalf("reject: %v", err)
		}
		if err := st.ApproveSSHKey(ctx, k.ID, "admin@x"); !errors.Is(err, ErrSSHKeyWrongState) {
			t.Errorf("approve after reject must refuse, got %v", err)
		}
		if err := st.RevokeSSHKey(ctx, k.ID, "admin@x"); !errors.Is(err, ErrSSHKeyWrongState) {
			t.Errorf("revoke of a rejected row must refuse, got %v", err)
		}
	})

	// The F-381 regression (goat#214): reject -> resubmit-the-same-key used to
	// keep the row `rejected` while the banner reported success — the queue
	// lists only `submitted`, so the key waited nowhere, forever.
	t.Run("resubmitting a rejected key reopens it as a fresh application", func(t *testing.T) {
		k := submit("heidi")
		if err := st.RejectSSHKey(ctx, k.ID, "admin@x"); err != nil {
			t.Fatalf("reject: %v", err)
		}
		again, err := st.SubmitSSHKey(ctx, &SSHKey{
			Handle: "heidi", KeyType: f[0], KeyBody: f[1], Comment: "label-fixed",
		}, 90)
		if err != nil {
			t.Fatalf("resubmit after reject: %v", err)
		}
		if again.ID != k.ID {
			t.Errorf("reopen created a second row (%s vs %s)", again.ID, k.ID)
		}
		if again.Status != SSHKeySubmitted {
			t.Fatalf("resubmit after reject left status %q, want %q", again.Status, SSHKeySubmitted)
		}
		if again.DecidedBy != "" || again.DecidedAt != 0 {
			t.Errorf("reopened row still carries the old decision: %+v", again)
		}
		if again.Comment != "label-fixed" {
			t.Errorf("reopen should refresh the comment, got %q", again.Comment)
		}
		// The point of the reopen: it is back in the queue and the gate works.
		queued, _ := st.ListSSHKeys(ctx, SSHKeySubmitted)
		found := false
		for _, q := range queued {
			found = found || q.ID == k.ID
		}
		if !found {
			t.Error("reopened key is not in the submitted queue")
		}
		if err := st.ApproveSSHKey(ctx, k.ID, "admin@x"); err != nil {
			t.Errorf("approve of a reopened key must work: %v", err)
		}
	})

	t.Run("a revoked key stays revoked on resubmit", func(t *testing.T) {
		k := submit("user903x")
		if err := st.RevokeSSHKey(ctx, k.ID, "admin@x"); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		again, err := st.SubmitSSHKey(ctx, &SSHKey{
			Handle: "user903x", KeyType: f[0], KeyBody: f[1],
		}, 90)
		if err != nil {
			t.Fatalf("resubmit after revoke: %v", err)
		}
		if again.Status != SSHKeyRevoked {
			t.Errorf("resubmit resurrected a REVOKED key to %q — revoked must stay cut", again.Status)
		}
	})

	// Revocation must never be the operation refused on a technicality — it
	// works from every live state.
	t.Run("revoke works from submitted, approved and installed", func(t *testing.T) {
		for i, prep := range []func(id string){
			func(string) {},
			func(id string) { _ = st.ApproveSSHKey(ctx, id, "a") },
			func(id string) { _ = st.ApproveSSHKey(ctx, id, "a"); _ = st.MarkSSHKeyInstalled(ctx, id, "a") },
		} {
			k := submit(strings.Repeat("d", i+1))
			prep(k.ID)
			if err := st.RevokeSSHKey(ctx, k.ID, "admin@x"); err != nil {
				t.Errorf("case %d: revoke failed: %v", i, err)
			}
			if got, _ := st.SSHKeyByID(ctx, k.ID); got.Status != SSHKeyRevoked {
				t.Errorf("case %d: status = %q, want revoked", i, got.Status)
			}
		}
	})

	// Re-submitting the same key must not create a second grant to reason about,
	// and must not drop an installed key back to submitted.
	t.Run("re-submission is an update, not a second grant", func(t *testing.T) {
		k := submit("erin")
		_ = st.ApproveSSHKey(ctx, k.ID, "a")
		_ = st.MarkSSHKeyInstalled(ctx, k.ID, "a")
		again, err := st.SubmitSSHKey(ctx, &SSHKey{
			Handle: "erin", KeyType: f[0], KeyBody: f[1], Comment: "renamed-laptop",
		}, 90)
		if err != nil {
			t.Fatalf("re-submit: %v", err)
		}
		if again.ID != k.ID {
			t.Errorf("re-submission created a second row (%s vs %s)", again.ID, k.ID)
		}
		if again.Status != SSHKeyInstalled {
			t.Errorf("re-submission reset an installed key to %q — it must not re-open the gate", again.Status)
		}
		if again.Comment != "renamed-laptop" {
			t.Errorf("cosmetic comment should refresh, got %q", again.Comment)
		}
	})

	t.Run("expiry is recorded when a ttl is given, absent when not", func(t *testing.T) {
		withTTL := submit("frank")
		if withTTL.ExpiresAt == 0 {
			t.Error("ttl>0 must record an expiry")
		}
		k, err := st.SubmitSSHKey(ctx, &SSHKey{
			Handle: "grace", KeyType: f[0], KeyBody: f[1],
		}, 0)
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		if k.ExpiresAt != 0 {
			t.Error("ttl=0 must record NO expiry rather than inventing one")
		}
	})

	t.Run("unknown id is not-found, distinct from a state refusal", func(t *testing.T) {
		if err := st.ApproveSSHKey(ctx, "sshk-ghost", "a"); !errors.Is(err, ErrNotFound) {
			t.Errorf("unknown id err = %v, want ErrNotFound", err)
		}
	})

	t.Run("skip-report applies only to approved keys and is state-neutral", func(t *testing.T) {
		k := submit("henry")
		// Skip on a submitted row must refuse as a wrong-state conflict.
		if err := st.RecordSSHKeySkip(ctx, k.ID, "installer", "no_manifest"); !errors.Is(err, ErrSSHKeyWrongState) {
			t.Errorf("skip-report on submitted row must refuse with ErrSSHKeyWrongState, got %v", err)
		}
		if err := st.RecordSSHKeySkip(ctx, "sshk-ghost", "installer", "no_scope"); !errors.Is(err, ErrNotFound) {
			t.Errorf("skip-report on unknown id must be ErrNotFound, got %v", err)
		}
		if err := st.ApproveSSHKey(ctx, k.ID, "admin@x"); err != nil {
			t.Fatalf("approve: %v", err)
		}
		if err := st.RecordSSHKeySkip(ctx, k.ID, "installer", "no_scope"); err != nil {
			t.Fatalf("skip-report on approved row: %v", err)
		}
		got, _ := st.SSHKeyByID(ctx, k.ID)
		if got.Status != SSHKeyApproved {
			t.Errorf("skip-report changed state to %q, want %q", got.Status, SSHKeyApproved)
		}
		if got.SkipReason != "no_scope" || got.SkipReportedBy != "installer" || got.SkipReportedAt == 0 {
			t.Errorf("skip-report fields not recorded correctly: %+v", got)
		}
	})

	t.Run("a bad key is refused at submit, never stored", func(t *testing.T) {
		if _, err := st.SubmitSSHKey(ctx, &SSHKey{
			Handle: "mallory", KeyType: "ssh-rsa", KeyBody: f[1],
		}, 0); !errors.Is(err, ErrSSHKeyBadFormat) {
			t.Errorf("type-spoofed key must be refused at submit, got %v", err)
		}
		ks, _ := st.ListSSHKeys(ctx, "")
		for _, k := range ks {
			if k.Handle == "mallory" {
				t.Error("a refused key was stored anyway")
			}
		}
	})

	t.Run("skip-report annotation is cleared on install and revoke, kept on identical resubmit", func(t *testing.T) {
		// install clears the annotation
		k := submit("irma")
		_ = st.ApproveSSHKey(ctx, k.ID, "admin@x")
		_ = st.RecordSSHKeySkip(ctx, k.ID, "installer", "no_manifest")
		if err := st.MarkSSHKeyInstalled(ctx, k.ID, "installer"); err != nil {
			t.Fatalf("install after skip-report: %v", err)
		}
		got, _ := st.SSHKeyByID(ctx, k.ID)
		if got.Status != SSHKeyInstalled || got.SkipReason != "" || got.SkipReportedBy != "" || got.SkipReportedAt != 0 {
			t.Errorf("install must clear skip annotation, got %+v", got)
		}

		// revoke clears the annotation from any live state
		k2 := submit("jane")
		_ = st.ApproveSSHKey(ctx, k2.ID, "admin@x")
		_ = st.RecordSSHKeySkip(ctx, k2.ID, "installer", "no_scope")
		if err := st.RevokeSSHKey(ctx, k2.ID, "admin@x"); err != nil {
			t.Fatalf("revoke after skip-report: %v", err)
		}
		got2, _ := st.SSHKeyByID(ctx, k2.ID)
		if got2.Status != SSHKeyRevoked || got2.SkipReason != "" || got2.SkipReportedBy != "" || got2.SkipReportedAt != 0 {
			t.Errorf("revoke must clear skip annotation, got %+v", got2)
		}

		// An identical resubmit KEEPS the annotation. Nothing about the skip
		// condition changed, so wiping the operator's diagnostic would blank the
		// reason for whoever triages the queue next — and the likeliest way to
		// reach this path is a person who read "not yet placed on the hosts",
		// assumed their submission failed, and re-pasted the same key.
		k3 := submit("kate")
		_ = st.ApproveSSHKey(ctx, k3.ID, "admin@x")
		_ = st.RecordSSHKeySkip(ctx, k3.ID, "installer", "no_manifest")
		again, err := st.SubmitSSHKey(ctx, &SSHKey{
			Handle: "kate", KeyType: f[0], KeyBody: f[1], Comment: "renamed-laptop",
		}, 90)
		if err != nil {
			t.Fatalf("re-submit after skip-report: %v", err)
		}
		if again.ID != k3.ID {
			t.Errorf("re-submission after skip-report created a second row (%s vs %s)", again.ID, k3.ID)
		}
		if again.Status != SSHKeyApproved {
			t.Errorf("re-submission must keep state %q, got %q", SSHKeyApproved, again.Status)
		}
		if again.SkipReason != "no_manifest" || again.SkipReportedBy != "installer" || again.SkipReportedAt == 0 {
			t.Errorf("identical re-submission must KEEP the skip annotation, got %+v", again)
		}
		if again.Comment != "renamed-laptop" {
			t.Errorf("identical re-submission must still refresh cosmetic fields, got comment %q", again.Comment)
		}
	})
}
