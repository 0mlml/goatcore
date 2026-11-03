// live_state.go — relay-side live-state reader for the wg-cp0
// orchestrator.
//
// The dry-run + apply pipeline needs to know which peers each relay's
// wg-cp0 listener currently holds in its in-memory allowlist. That
// information lives in `wg show wg-cp0 dump` on each relay; the
// reader fans out over SSH-over-wg-cp0 with bounded concurrency,
// parses the dump into LiveState, and tolerates per-relay reachability
// failures (the per-relay row records UnreachableErr; the diff
// formatter surfaces "WOULD SKIP relay-X" so the operator can decide
// whether partial-fleet coverage is acceptable).
//
// The interface is small on purpose:
//
//   type LiveStateReader interface {
//       Read(ctx context.Context, relays []peerset.Relay) (LiveState, error)
//   }
//
// B and C unit-test their planners against a MockLiveStateReader (a
// thin struct that records calls and returns a canned LiveState).
// Production code uses NewSSHLiveStateReader which dials each relay
// over SSH-agent auth (no new key custody — the operator's already
// authenticated to the wg-cp0 mesh).
//
// Bounded concurrency is per-Reader (default 4); the 5s per-relay
// timeout is a per-call default (override via SSHReaderOptions).
// Partial failures are non-fatal — the LiveState carries a per-relay
// Unreachable flag + error string.

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// LiveStateReader is the mock-friendly seam between the orchestrator
// and the SSH fan-out. Plan calls Read once per invocation. Returning
// (LiveState, nil) with per-relay Unreachable flags is the normal
// partial-failure path; returning a non-nil error means the read
// could not even attempt the fan-out (e.g., zero relays in PeerSet).
type LiveStateReader interface {
	Read(ctx context.Context, relays []peerset.Relay) (LiveState, error)
}

// LiveState is the parsed per-relay wg-set state. Each map key is a
// peerset.Relay.Name; missing relays in the map were not reached.
type LiveState struct {
	// PerRelay maps relay name → that relay's parsed wg show dump.
	PerRelay map[string]*RelayLiveState

	// FetchedAt is the wall-clock at the start of the fan-out.
	FetchedAt time.Time
}

// RelayLiveState is one relay's wg-cp0 listener state at fetch time.
type RelayLiveState struct {
	// Peers is keyed by base64 pubkey — the same shape `wg show dump`
	// emits, so a pubkey-equality check is a map lookup.
	Peers map[string]LivePeer

	// Reachable is true when the SSH dial + wg-show round-trip
	// succeeded. False here means the orchestrator must surface a
	// "WOULD SKIP relay-X (unreachable: <err>)" line in the dry-run.
	Reachable bool

	// UnreachableErr captures the wrapping error for the audit
	// record when Reachable is false. Empty when Reachable is true.
	UnreachableErr string

	// FetchDurationMs is the per-relay timing the audit record
	// emits (debugging signal: a slow read is a clue the relay is
	// network-degraded even when the read eventually succeeded).
	FetchDurationMs int64
}

// LivePeer is one row from `wg show wg-cp0 dump`. The dump format is:
//
//	<interface-public-key>\t<private-key-or-(none)>\t<listen-port>\t<fwmark>     (header line, dropped)
//	<peer-pubkey>\t<preshared>\t<endpoint>\t<allowed-ips>\t<latest-handshake>\t<rx>\t<tx>\t<keepalive>
//
// We keep only the fields the orchestrator needs.
type LivePeer struct {
	Pubkey          string
	AllowedIPs      string    // first /32 — the wg-cp0 mesh address
	LastHandshake   time.Time // zero when never handshaked
	TransferRxBytes int64
	TransferTxBytes int64
}

// SSHLiveStateReader is the production LiveStateReader. SSH-agent
// auth, bounded concurrency, per-relay timeout. Constructed via
// NewSSHLiveStateReader.
type SSHLiveStateReader struct {
	opts SSHReaderOptions
}

// SSHReaderOptions configures the SSH-backed live-state reader.
type SSHReaderOptions struct {
	// Username is the SSH user on the relays. Defaults to
	// "ubuntu" (the operator-set on every wg-cp0 relay today).
	Username string

	// Concurrency caps in-flight SSH dials. Defaults to 4.
	Concurrency int

	// PerRelayTimeout caps the dial+exec+read of one relay's wg-show.
	// Defaults to 5s.
	PerRelayTimeout time.Duration

	// EndpointFromRelay maps a peerset.Relay to a "host:port" string
	// the ssh.Dial call uses. Defaults to relay.MeshAddress + ":22"
	// (SSH-over-wg-cp0 — operator dials the relay's wg-cp0 mesh
	// address, which works only when the operator is themselves a
	// wg-cp0 peer; this is the documented operator-laptop posture).
	EndpointFromRelay func(peerset.Relay) string

	// AgentSocket is the SSH_AUTH_SOCK to dial for agent auth.
	// Defaults to os.Getenv("SSH_AUTH_SOCK"). Tests inject a fake.
	AgentSocket string

	// HostKeyCallback governs host-key verification. Defaults to
	// ssh.InsecureIgnoreHostKey() because the wg-cp0 tunnel is the
	// authenticated substrate (ADR 0313 silent control plane — only
	// authenticated wg-cp0 peers can reach the relay's :22 over the
	// mesh address). Production hardening can pass a known_hosts
	// callback via this hook; the default does not undermine the
	// substrate's posture.
	HostKeyCallback ssh.HostKeyCallback
}

// NewSSHLiveStateReader constructs the production reader. Pass an
// empty SSHReaderOptions to accept all defaults.
func NewSSHLiveStateReader(opts SSHReaderOptions) *SSHLiveStateReader {
	if opts.Username == "" {
		opts.Username = "ubuntu"
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.PerRelayTimeout <= 0 {
		opts.PerRelayTimeout = 5 * time.Second
	}
	if opts.EndpointFromRelay == nil {
		opts.EndpointFromRelay = defaultEndpointFromRelay
	}
	if opts.AgentSocket == "" {
		opts.AgentSocket = os.Getenv("SSH_AUTH_SOCK")
	}
	if opts.HostKeyCallback == nil {
		// See doc on SSHReaderOptions.HostKeyCallback for why this is
		// the safe default in the wg-cp0 trust model.
		opts.HostKeyCallback = ssh.InsecureIgnoreHostKey() //nolint:gosec // wg-cp0 is the auth layer
	}
	return &SSHLiveStateReader{opts: opts}
}

// defaultEndpointFromRelay maps a relay to its SSH-over-wg-cp0 host:port.
// The mesh address is what the operator laptop reaches; falling back
// to the relay's public Endpoint would cross outside the silent
// control plane and is intentionally not the default.
func defaultEndpointFromRelay(r peerset.Relay) string {
	if r.MeshAddress != "" {
		return r.MeshAddress + ":22"
	}
	return ""
}

// Read implements LiveStateReader.
func (r *SSHLiveStateReader) Read(ctx context.Context, relays []peerset.Relay) (LiveState, error) {
	if len(relays) == 0 {
		return LiveState{
			PerRelay:  map[string]*RelayLiveState{},
			FetchedAt: time.Now(),
		}, nil
	}

	// Build the agent-backed auth method once; reused across all
	// per-relay dials. A missing SSH_AUTH_SOCK is reported as a
	// reader-level error rather than a per-relay one because every
	// relay would fail the same way.
	signers, err := r.loadAgentSigners()
	if err != nil {
		return LiveState{}, fmt.Errorf("wg-cp0 live-state: ssh agent: %w", err)
	}

	return r.readWithSigners(ctx, relays, signers), nil
}

// readWithSigners is the inner fan-out separated from Read so unit
// tests can exercise the per-relay goroutine + dial-failure path
// without standing up a real ssh-agent. Callers are expected to have
// already resolved signers (Read is the production entry point).
func (r *SSHLiveStateReader) readWithSigners(ctx context.Context, relays []peerset.Relay, signers []ssh.Signer) LiveState {
	out := LiveState{
		PerRelay:  make(map[string]*RelayLiveState, len(relays)),
		FetchedAt: time.Now(),
	}
	var mu sync.Mutex
	sem := make(chan struct{}, r.opts.Concurrency)
	var wg sync.WaitGroup

	for i := range relays {
		relay := relays[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				out.PerRelay[relay.Name] = &RelayLiveState{
					Reachable:      false,
					UnreachableErr: ctx.Err().Error(),
				}
				mu.Unlock()
				return
			}
			defer func() { <-sem }()
			state := r.readOne(ctx, relay, signers)
			mu.Lock()
			out.PerRelay[relay.Name] = state
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// loadAgentSigners reads signer identities from the SSH agent socket.
func (r *SSHLiveStateReader) loadAgentSigners() ([]ssh.Signer, error) {
	if r.opts.AgentSocket == "" {
		return nil, errors.New("SSH_AUTH_SOCK not set; agent auth required")
	}
	conn, err := net.Dial("unix", r.opts.AgentSocket)
	if err != nil {
		return nil, fmt.Errorf("dial agent: %w", err)
	}
	// The connection lifetime is owned by the agent client; closing
	// it here would invalidate the signers. The OS reaps on process
	// exit. (The Reader is constructed once per orchestrator
	// invocation; there is no leak across long-running daemons.)
	ag := agent.NewClient(conn)
	signers, err := ag.Signers()
	if err != nil {
		return nil, fmt.Errorf("list agent signers: %w", err)
	}
	if len(signers) == 0 {
		return nil, errors.New("ssh agent has no signers; run ssh-add first")
	}
	return signers, nil
}

// readOne dials one relay, runs `wg show wg-cp0 dump`, parses, returns.
func (r *SSHLiveStateReader) readOne(ctx context.Context, relay peerset.Relay, signers []ssh.Signer) *RelayLiveState {
	state := &RelayLiveState{
		Peers:     map[string]LivePeer{},
		Reachable: false,
	}
	endpoint := r.opts.EndpointFromRelay(relay)
	if endpoint == "" {
		state.UnreachableErr = fmt.Sprintf("relay %s has no usable SSH endpoint (mesh_address empty)", relay.Name)
		return state
	}

	dialCtx, cancel := context.WithTimeout(ctx, r.opts.PerRelayTimeout)
	defer cancel()

	cfg := &ssh.ClientConfig{
		User:            r.opts.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signers...)},
		HostKeyCallback: r.opts.HostKeyCallback,
		Timeout:         r.opts.PerRelayTimeout,
	}

	start := time.Now()
	dialer := net.Dialer{Timeout: r.opts.PerRelayTimeout}
	tcpConn, err := dialer.DialContext(dialCtx, "tcp", endpoint)
	if err != nil {
		state.UnreachableErr = fmt.Sprintf("dial %s: %v", endpoint, err)
		state.FetchDurationMs = time.Since(start).Milliseconds()
		return state
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, endpoint, cfg)
	if err != nil {
		_ = tcpConn.Close()
		state.UnreachableErr = fmt.Sprintf("ssh handshake %s: %v", endpoint, err)
		state.FetchDurationMs = time.Since(start).Milliseconds()
		return state
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close() //nolint:errcheck // probe result is determined before SSH client cleanup

	session, err := client.NewSession()
	if err != nil {
		state.UnreachableErr = fmt.Sprintf("ssh session %s: %v", relay.Name, err)
		state.FetchDurationMs = time.Since(start).Milliseconds()
		return state
	}
	defer session.Close() //nolint:errcheck // probe result is determined before SSH session cleanup

	out, err := session.Output("sudo wg show wg-cp0 dump")
	if err != nil {
		state.UnreachableErr = fmt.Sprintf("wg show %s: %v", relay.Name, err)
		state.FetchDurationMs = time.Since(start).Milliseconds()
		return state
	}

	peers, err := ParseWGShowDump(strings.NewReader(string(out)))
	if err != nil {
		state.UnreachableErr = fmt.Sprintf("parse wg dump %s: %v", relay.Name, err)
		state.FetchDurationMs = time.Since(start).Milliseconds()
		return state
	}
	state.Peers = peers
	state.Reachable = true
	state.FetchDurationMs = time.Since(start).Milliseconds()
	return state
}

// ParseWGShowDump parses the output of `wg show <iface> dump`. The
// first line is the interface row (private key, listen port, fwmark);
// every subsequent line is one peer. Exported so unit tests + the
// integration harness can re-use the parser without an SSH round-trip.
//
// Format reference: wg(8) "show ... dump" section.
//
// Per-peer columns (tab-separated):
//
//	0  peer public key (base64)
//	1  preshared-key or "(none)"
//	2  endpoint "host:port" or "(none)"
//	3  allowed-ips, comma-separated
//	4  latest-handshake unix-seconds (0 when never)
//	5  transfer-rx bytes
//	6  transfer-tx bytes
//	7  persistent-keepalive seconds or "off"
func ParseWGShowDump(r io.Reader) (map[string]LivePeer, error) {
	peers := map[string]LivePeer{}
	scanner := bufio.NewScanner(r)
	// Allow long lines: a real-world wg dump line is well under 4 KiB,
	// but the bufio default of 64 KiB is plenty.
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			// Interface row — skip.
			first = false
			continue
		}
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 4 {
			return nil, fmt.Errorf("wg dump: short row %q", line)
		}
		pubkey := cols[0]
		if pubkey == "" {
			return nil, fmt.Errorf("wg dump: empty pubkey on row %q", line)
		}
		// AllowedIPs may be a single /32 or a comma-list; we keep
		// the raw column so the diff formatter can compare against
		// the planned value byte-for-byte. Production wg-cp0 today
		// canonically writes "<mesh-addr>/32,198.18.0.0/24" per
		// ADR 0321; older bundles emitted just "<mesh-addr>/32".
		allowed := cols[3]

		peer := LivePeer{
			Pubkey:     pubkey,
			AllowedIPs: allowed,
		}
		if len(cols) >= 5 {
			if secs, err := strconv.ParseInt(cols[4], 10, 64); err == nil && secs > 0 {
				peer.LastHandshake = time.Unix(secs, 0).UTC()
			}
		}
		if len(cols) >= 6 {
			if v, err := strconv.ParseInt(cols[5], 10, 64); err == nil {
				peer.TransferRxBytes = v
			}
		}
		if len(cols) >= 7 {
			if v, err := strconv.ParseInt(cols[6], 10, 64); err == nil {
				peer.TransferTxBytes = v
			}
		}
		peers[pubkey] = peer
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("wg dump scan: %w", err)
	}
	return peers, nil
}

// SSHRelayMutator is the production RelayMutator — runs `wg set ...`
// over SSH-over-wg-cp0 on each relay using ssh-agent auth. Composes
// with the same SSHReaderOptions as SSHLiveStateReader so an operator
// laptop's ssh-agent serves both Plan and Apply phases without
// re-prompting.
type SSHRelayMutator struct {
	opts SSHReaderOptions
}

// NewSSHRelayMutator constructs the production mutator. Defaults
// match SSHLiveStateReader so the same options struct works for both
// pieces.
func NewSSHRelayMutator(opts SSHReaderOptions) *SSHRelayMutator {
	if opts.Username == "" {
		opts.Username = "ubuntu"
	}
	if opts.PerRelayTimeout <= 0 {
		opts.PerRelayTimeout = 5 * time.Second
	}
	if opts.EndpointFromRelay == nil {
		opts.EndpointFromRelay = defaultEndpointFromRelay
	}
	if opts.AgentSocket == "" {
		opts.AgentSocket = os.Getenv("SSH_AUTH_SOCK")
	}
	if opts.HostKeyCallback == nil {
		opts.HostKeyCallback = ssh.InsecureIgnoreHostKey() //nolint:gosec // wg-cp0 is the auth layer
	}
	return &SSHRelayMutator{opts: opts}
}

// AddPeer implements RelayMutator.
func (m *SSHRelayMutator) AddPeer(ctx context.Context, relay peerset.Relay, pubkey, allowedIPs string, keepalive int) error {
	cmd := fmt.Sprintf("sudo wg set wg-cp0 peer %s allowed-ips %s persistent-keepalive %d",
		shellEscape(pubkey), shellEscape(allowedIPs), keepalive)
	return m.execOne(ctx, relay, cmd)
}

// RemovePeer implements RelayMutator.
func (m *SSHRelayMutator) RemovePeer(ctx context.Context, relay peerset.Relay, pubkey string) error {
	cmd := fmt.Sprintf("sudo wg set wg-cp0 peer %s remove", shellEscape(pubkey))
	return m.execOne(ctx, relay, cmd)
}

// PersistConfig implements RelayMutator.
func (m *SSHRelayMutator) PersistConfig(ctx context.Context, relay peerset.Relay) error {
	return m.execOne(ctx, relay, "sudo wg-quick save wg-cp0")
}

// execOne dials the relay once + runs the given command. Returns the
// stderr of the command on non-zero exit so failure-mode triage from
// the audit log is straightforward.
func (m *SSHRelayMutator) execOne(ctx context.Context, relay peerset.Relay, command string) error {
	endpoint := m.opts.EndpointFromRelay(relay)
	if endpoint == "" {
		return fmt.Errorf("relay %s has no usable SSH endpoint (mesh_address empty)", relay.Name)
	}
	signers, err := m.loadAgentSigners()
	if err != nil {
		return fmt.Errorf("ssh agent: %w", err)
	}

	dialCtx, cancel := context.WithTimeout(ctx, m.opts.PerRelayTimeout)
	defer cancel()

	cfg := &ssh.ClientConfig{
		User:            m.opts.Username,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signers...)},
		HostKeyCallback: m.opts.HostKeyCallback,
		Timeout:         m.opts.PerRelayTimeout,
	}

	dialer := net.Dialer{Timeout: m.opts.PerRelayTimeout}
	tcpConn, err := dialer.DialContext(dialCtx, "tcp", endpoint)
	if err != nil {
		return fmt.Errorf("dial %s: %w", endpoint, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, endpoint, cfg)
	if err != nil {
		_ = tcpConn.Close()
		return fmt.Errorf("ssh handshake %s: %w", endpoint, err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close() //nolint:errcheck // command result is determined before SSH client cleanup

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session %s: %w", relay.Name, err)
	}
	defer session.Close() //nolint:errcheck // command result is determined before SSH session cleanup

	out, err := session.CombinedOutput(command)
	if err != nil {
		return fmt.Errorf("relay %s: %s: %w (out: %s)", relay.Name, command, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// loadAgentSigners mirrors the reader's helper; duplicated rather
// than pulled into a common type to keep the two SSH consumers
// independently constructable.
func (m *SSHRelayMutator) loadAgentSigners() ([]ssh.Signer, error) {
	if m.opts.AgentSocket == "" {
		return nil, errors.New("SSH_AUTH_SOCK not set; agent auth required")
	}
	conn, err := net.Dial("unix", m.opts.AgentSocket)
	if err != nil {
		return nil, fmt.Errorf("dial agent: %w", err)
	}
	ag := agent.NewClient(conn)
	signers, err := ag.Signers()
	if err != nil {
		return nil, fmt.Errorf("list agent signers: %w", err)
	}
	if len(signers) == 0 {
		return nil, errors.New("ssh agent has no signers; run ssh-add first")
	}
	return signers, nil
}

// shellEscape wraps the given string in single quotes after escaping
// any embedded single quotes. wg pubkeys are base64 so they cannot
// contain `'` in normal operation; the escape is defense-in-depth.
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// MockLiveStateReader is the in-memory reader for unit tests. Set
// State + Err; Read returns them verbatim. Calls is appended to on
// every Read so tests can assert call shape.
type MockLiveStateReader struct {
	State LiveState
	Err   error
	Calls [][]peerset.Relay
	mu    sync.Mutex
}

// Read implements LiveStateReader.
func (m *MockLiveStateReader) Read(_ context.Context, relays []peerset.Relay) (LiveState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, relays)
	if m.Err != nil {
		return LiveState{}, m.Err
	}
	return m.State, nil
}

// RelayMutator is the seam for wire-side wg-set operations. The
// Applier calls it from inside concrete Mutation impls; tests inject
// a MockRelayMutator that records calls without touching SSH.
type RelayMutator interface {
	// AddPeer runs `wg set <iface> peer <pubkey> allowed-ips <ips>
	// persistent-keepalive <kp>` on the named relay. Idempotent on
	// the relay side (wg set rewrites the peer's row).
	AddPeer(ctx context.Context, relay peerset.Relay, pubkey, allowedIPs string, keepalive int) error

	// RemovePeer runs `wg set <iface> peer <pubkey> remove` on the
	// named relay. Idempotent (removing an absent peer succeeds).
	RemovePeer(ctx context.Context, relay peerset.Relay, pubkey string) error

	// PersistConfig runs `wg-quick save <iface>` on the named relay
	// so the in-memory change survives reboot. Called once per relay
	// per Apply, after all AddPeer / RemovePeer calls for that relay
	// have succeeded.
	PersistConfig(ctx context.Context, relay peerset.Relay) error
}

// MockRelayMutator is the in-memory mutator for unit tests.
type MockRelayMutator struct {
	AddCalls     []MockAddCall
	RemoveCalls  []MockRemoveCall
	PersistCalls []string // relay name
	AddErr       error
	RemoveErr    error
	PersistErr   error
	mu           sync.Mutex
}

// MockAddCall records one AddPeer invocation.
type MockAddCall struct {
	Relay      string
	Pubkey     string
	AllowedIPs string
	Keepalive  int
}

// MockRemoveCall records one RemovePeer invocation.
type MockRemoveCall struct {
	Relay  string
	Pubkey string
}

// AddPeer implements RelayMutator.
func (m *MockRelayMutator) AddPeer(_ context.Context, relay peerset.Relay, pubkey, allowedIPs string, keepalive int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AddCalls = append(m.AddCalls, MockAddCall{
		Relay: relay.Name, Pubkey: pubkey, AllowedIPs: allowedIPs, Keepalive: keepalive,
	})
	return m.AddErr
}

// RemovePeer implements RelayMutator.
func (m *MockRelayMutator) RemovePeer(_ context.Context, relay peerset.Relay, pubkey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RemoveCalls = append(m.RemoveCalls, MockRemoveCall{Relay: relay.Name, Pubkey: pubkey})
	return m.RemoveErr
}

// PersistConfig implements RelayMutator.
func (m *MockRelayMutator) PersistConfig(_ context.Context, relay peerset.Relay) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PersistCalls = append(m.PersistCalls, relay.Name)
	return m.PersistErr
}
