package onboardstore

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const validRestoreManifest = `{"identity":{"handle":"legacy","email":"legacy@example.test","name":"Legacy","status":"active","added":"2026-06-01","added_by":"admin@example.test"},"netbird":{"enabled":false},"ory_site":{"enabled":false},"ory_idp":{"enabled":false},"zenoh":{"enabled":false}}`

func TestRestoreAndRetryRefuseInvalidManifestWithoutChanges(t *testing.T) {
	for index, manifest := range []string{"{}", "", "not json", "null", "[]", `{"netbird":{"enabled":false}}`} {
		for _, status := range []string{ProposalArchived, ProposalFailed} {
			t.Run(fmt.Sprintf("%s/invalid-%d", status, index), func(t *testing.T) {
				ctx := context.Background()
				st := openTest(t)
				p := &Proposal{ID: "legacy", Handle: "legacy", Email: "legacy@example.test", Env: "zagros",
					Status: status, ManifestJSON: manifest, SetupKey: "old-synthetic-key", BundlePath: "/old-bundle"}
				if err := st.CreateProposal(ctx, p); err != nil {
					t.Fatal(err)
				}
				if err := st.SetProposalResult(ctx, p.ID, status, p.SetupKey, "", p.BundlePath, "", "", false); err != nil {
					t.Fatal(err)
				}
				before, err := st.GetProposal(ctx, p.ID)
				if err != nil {
					t.Fatal(err)
				}
				if status == ProposalArchived {
					moved, err := st.RestoreProposal(ctx, p.ID, "admin@example.test")
					if err == nil || moved {
						t.Errorf("invalid restore: moved=%v err=%v", moved, err)
					}
				} else if err := st.RetryProposal(ctx, p.ID); err == nil {
					t.Error("invalid retry was accepted")
				}
				after, err := st.GetProposal(ctx, p.ID)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Errorf("refused operation changed record: before=%+v after=%+v", before, after)
				}
			})
		}
	}
}

func TestRestoreAndRetryAcceptValidMeshAndNoMesh(t *testing.T) {
	mesh := strings.Replace(validRestoreManifest, `"netbird":{"enabled":false}`,
		`"netbird":{"enabled":true,"sites":["zagros"],"role":"peer","auto_groups":["OBSERVER"]}`, 1)
	for _, manifest := range []string{validRestoreManifest, mesh} {
		for _, status := range []string{ProposalArchived, ProposalFailed} {
			t.Run(status+"/"+manifest, func(t *testing.T) {
				ctx := context.Background()
				st := openTest(t)
				p := &Proposal{ID: "legacy", Handle: "legacy", Email: "legacy@example.test", Env: "zagros", Status: status, ManifestJSON: manifest}
				if err := st.CreateProposal(ctx, p); err != nil {
					t.Fatal(err)
				}
				if status == ProposalArchived {
					if moved, err := st.RestoreProposal(ctx, p.ID, "admin@example.test"); err != nil || !moved {
						t.Fatalf("valid restore: moved=%v err=%v", moved, err)
					}
				} else if err := st.RetryProposal(ctx, p.ID); err != nil {
					t.Fatal(err)
				}
				got, err := st.GetProposal(ctx, p.ID)
				if err != nil || got.Status != ProposalApproved || got.ManifestJSON != manifest {
					t.Fatalf("valid record did not queue unchanged: %+v (%v)", got, err)
				}
			})
		}
	}
}
