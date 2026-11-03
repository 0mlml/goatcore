// Command harness is the wg-cp0 combinatorial mesh-reach test driver
// (ADR 1017 first instance applied at bench-env scope, per the ADR 0321
// §"Implementation" 22R.0.2 acceptance criterion).
//
// Invariant under test
//
//	Any peer X can reach any peer Y over wg-cp0, for every (X, Y) pair
//	in an N-peer × M-relay deployment, regardless of bundle ordering or
//	per-bundle relay selection. A regression of this invariant is a
//	release-blocking failure on any wg-cp0-touching change.
//
// What it does
//
//  1. Builds the bench container image (Dockerfile in the parent dir).
//  2. Generates a throwaway dev-mode offline CA (ECDSA P-256 per Block 79).
//  3. Generates M Curve25519 server keypairs for the relays.
//  4. Writes a single relays.json describing all M relays (the same one
//     every bundle-create invocation reads).
//  5. For each peer i ∈ [0, N): invokes the real `bundle-create` binary
//     (built from the current tree) to mint a signed CBOR bundle.
//  6. Optionally (--regression mode): post-processes each bundle to
//     drift AllowedIPs into the pre-ADR-0321 asymmetric shape (catchall
//     placed on a different relay per peer), then re-signs with the dev
//     CA. The mesh-reach matrix is expected to FAIL in this mode — that
//     is how we demonstrate the harness would have caught F-116.
//  7. For each peer i: extracts the wg-cp0 pubkey from the bundle CBOR.
//  8. For each peer i: invokes `wg-cp0-bundle-apply` to render the
//     device-side wg-cp0.conf + privkey under /work/peer-i/.
//  9. For each relay j: renders the server-side wg-cp0.conf naming every
//     peer as an explicit [Peer] block (single /32 per peer — symmetric
//     by construction on the relay side).
//  10. docker run all N+M containers attached to a single bridge network;
//     each container's entrypoint runs `wg-quick up wg-cp0`.
//  11. Cross-product ping: for every (src, dst) peer pair where src ≠ dst,
//     docker exec ping inside src targeting dst's wg-cp0 mesh address.
//  12. Tear down containers and the bridge network.
//  13. Emit a JSON verdict to --json-out (default stdout). Exits 0 on
//     PASS, exits 1 on FAIL.
//
// Operator-facing CLI: see --help. The expected invocations are
// `run.sh` (PASS expected) and `run-regression.sh` (FAIL expected),
// which wrap this binary with the right flag combinations.
package main

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
	"golang.org/x/crypto/curve25519"

	"github.com/usareur-af/goatcore/ops/enrollment/bundle"
	"github.com/usareur-af/goatcore/ops/enrollment/ca"
)

const (
	fleetSubnetCIDR     = bundle.WGCP0FleetSubnet // 198.18.0.0/24
	relayFirstHostByte  = 2                       // 198.18.0.2 .. 4
	peerFirstHostByte   = 10                      // 198.18.0.10 ..
	bridgeNetworkCIDR   = "172.30.0.0/24"
	relayBridgeBaseByte = 10  // 172.30.0.10 .. 12
	peerBridgeBaseByte  = 100 // 172.30.0.100 ..
	wgListenPort        = 51821
	defaultPingTimeout  = 3
	defaultPingCount    = 2
)

type harnessArgs struct {
	N                int
	M                int
	Regression       bool
	Image            string
	NetworkName      string
	WorkDir          string
	BundleCreatePath string
	BundleApplyPath  string
	KeepContainers   bool
	JSONOut          string
	Verbose          bool
	Now              time.Time
}

// matrixResult is one cell of the cross-product ping result.
type matrixResult struct {
	Src    string `json:"src"`
	Dst    string `json:"dst"`
	DstIP  string `json:"dst_ip"`
	OK     bool   `json:"ok"`
	Output string `json:"output,omitempty"`
}

// verdict is the top-level JSON emitted by the harness.
type verdict struct {
	Schema     string         `json:"schema"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
	N          int            `json:"n_peers"`
	M          int            `json:"m_relays"`
	Mode       string         `json:"mode"` // "symmetric" | "regression-asymmetric"
	Pass       bool           `json:"pass"`
	Total      int            `json:"total_pairs"`
	Failed     int            `json:"failed_pairs"`
	Matrix     []matrixResult `json:"matrix"`
	Note       string         `json:"note,omitempty"`
}

func main() {
	args := harnessArgs{Now: time.Now()}
	flag.IntVar(&args.N, "peers", 4, "Number of client peer containers (N).")
	flag.IntVar(&args.M, "relays", 3, "Number of relay containers (M).")
	flag.BoolVar(&args.Regression, "regression", false, "Deliberately drift bundles asymmetric (pre-ADR-0321) and assert the harness FAILS. PASS in this mode = harness is broken; the regression case proves it would have caught F-116.")
	flag.StringVar(&args.Image, "image", "wgcp0-bench:local", "Container image tag to use. Built locally if --build-image is set or if absent.")
	flag.StringVar(&args.NetworkName, "network", "wgcp0-bench", "Docker bridge network name.")
	flag.StringVar(&args.WorkDir, "work-dir", "", "Working directory for generated bundles + configs. Default: tempdir.")
	flag.StringVar(&args.BundleCreatePath, "bundle-create", "", "Path to bundle-create binary. Built from ../enrollment/cmd/bundle-create if empty.")
	flag.StringVar(&args.BundleApplyPath, "bundle-apply", "", "Path to wg-cp0-bundle-apply binary. Built from ../enrollment/cmd/wg-cp0-bundle-apply if empty.")
	flag.BoolVar(&args.KeepContainers, "keep-containers", false, "Skip teardown on exit (debug).")
	flag.StringVar(&args.JSONOut, "json-out", "-", "Write the JSON verdict here. '-' = stdout.")
	flag.BoolVar(&args.Verbose, "verbose", false, "Print phase logs to stderr.")
	flag.Parse()

	if args.N < 2 {
		fatal("--peers must be >= 2; got %d", args.N)
	}
	if args.M < 1 {
		fatal("--relays must be >= 1; got %d", args.M)
	}
	if args.M > 250 || args.N > 200 {
		fatal("too many peers/relays — keep N<=200 M<=250 (subnet exhaustion)")
	}

	code, err := runHarness(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harness: %v\n", err)
		os.Exit(2)
	}
	os.Exit(code)
}

// runHarness is the thin wrapper that ensures deferred cleanup runs
// regardless of the matrix outcome. The body returns an error; we
// translate to an exit code AFTER the function returns and the
// deferred container/network teardown has fired.
//
//	0 — matrix PASSED
//	1 — matrix FAILED (cells with ok=false present in the verdict)
//	2 — harness error before/during matrix execution
func runHarness(a harnessArgs) (int, error) {
	v := verdict{
		Schema:    "wg-cp0-mesh-reach.v1",
		StartedAt: a.Now,
		N:         a.N,
		M:         a.M,
		Mode:      "symmetric",
	}
	if err := runHarnessBody(a, &v); err != nil {
		return 2, err
	}
	if !v.Pass {
		return 1, nil
	}
	return 0, nil
}

func runHarnessBody(a harnessArgs, v *verdict) error {
	if a.Regression {
		v.Mode = "regression-asymmetric"
	}

	if a.WorkDir == "" {
		d, err := os.MkdirTemp("", "wgcp0-mesh-reach-")
		if err != nil {
			return fmt.Errorf("tempdir: %w", err)
		}
		a.WorkDir = d
	}
	logf(a, "work-dir: %s", a.WorkDir)

	// Phase 1: build the bench container image if not present.
	if err := ensureImage(a); err != nil {
		return fmt.Errorf("ensure image: %w", err)
	}

	// Phase 2: build bundle-create and wg-cp0-bundle-apply from current
	// tree if paths not provided. Catches issuer/device drift on every run.
	if a.BundleCreatePath == "" {
		p, err := buildGoBinary(a, "bundle-create", "../enrollment/cmd/bundle-create")
		if err != nil {
			return fmt.Errorf("build bundle-create: %w", err)
		}
		a.BundleCreatePath = p
	}
	if a.BundleApplyPath == "" {
		p, err := buildGoBinary(a, "wg-cp0-bundle-apply", "../enrollment/cmd/wg-cp0-bundle-apply")
		if err != nil {
			return fmt.Errorf("build wg-cp0-bundle-apply: %w", err)
		}
		a.BundleApplyPath = p
	}

	// Phase 3: generate a throwaway dev CA.
	caKeyPath, ageIDPath, caID, caPubPEM, caSigner, err := genDevCA(a)
	if err != nil {
		return fmt.Errorf("dev CA: %w", err)
	}
	logf(a, "dev CA: %s", caID)

	// Phase 4: generate M relay Curve25519 keypairs + relays.json.
	relays, err := genRelays(a)
	if err != nil {
		return fmt.Errorf("relays: %w", err)
	}
	relaysJSONPath := filepath.Join(a.WorkDir, "relays.json")
	if err := writeRelaysJSON(relaysJSONPath, relays); err != nil {
		return fmt.Errorf("write relays.json: %w", err)
	}

	// Phase 5: mint a bundle per peer via the real bundle-create binary.
	peers, err := mintBundles(a, relaysJSONPath, caKeyPath, ageIDPath, caID)
	if err != nil {
		return fmt.Errorf("mint bundles: %w", err)
	}

	// Phase 6 (regression mode only): drift each bundle's AllowedIPs to
	// the pre-ADR-0321 asymmetric shape and re-sign.
	if a.Regression {
		if err := driftBundlesAsymmetric(a, peers, caSigner); err != nil {
			return fmt.Errorf("drift bundles: %w", err)
		}
		v.Note = "regression mode: bundles deliberately drifted to pre-ADR-0321 asymmetric AllowedIPs; FAIL is the expected outcome"
	}

	// Phase 7: extract each peer's wg-cp0 pubkey from its bundle.
	for i := range peers {
		b, err := readBundle(peers[i].BundlePath)
		if err != nil {
			return fmt.Errorf("re-read bundle for %s: %w", peers[i].Name, err)
		}
		if len(b.CPDevicePubkey) == 0 {
			return fmt.Errorf("peer %s: bundle missing CPDevicePubkey", peers[i].Name)
		}
		peers[i].WGPubB64 = base64.StdEncoding.EncodeToString(b.CPDevicePubkey)
	}

	// Phase 8: render each peer's wg-cp0.conf via wg-cp0-bundle-apply.
	for i := range peers {
		if err := renderPeerConf(a, &peers[i], caPubPEM); err != nil {
			return fmt.Errorf("render peer %s conf: %w", peers[i].Name, err)
		}
	}

	// Phase 9: render each relay's wg-cp0.conf naming every peer.
	for i := range relays {
		if err := renderRelayConf(a, &relays[i], peers); err != nil {
			return fmt.Errorf("render relay %s conf: %w", relays[i].Name, err)
		}
	}

	// Phase 10: bring up the bridge network + N+M containers.
	if err := dockerNetworkCreate(a); err != nil {
		return fmt.Errorf("network create: %w", err)
	}
	if !a.KeepContainers {
		defer dockerNetworkRemove(a)
	}

	for i := range relays {
		if err := dockerRunRelay(a, &relays[i]); err != nil {
			return fmt.Errorf("start relay %s: %w", relays[i].Name, err)
		}
		if !a.KeepContainers {
			defer dockerStopRemove(a, relays[i].ContainerName)
		}
	}
	for i := range peers {
		if err := dockerRunPeer(a, &peers[i]); err != nil {
			return fmt.Errorf("start peer %s: %w", peers[i].Name, err)
		}
		if !a.KeepContainers {
			defer dockerStopRemove(a, peers[i].ContainerName)
		}
	}

	// Give wg-quick a moment to settle on every container. We wait for
	// the per-container readiness marker in /tmp/ready or just sleep.
	if err := waitContainersReady(a, peers, relays); err != nil {
		return fmt.Errorf("containers not ready: %w", err)
	}

	// Phase 11: cross-product ping matrix.
	v.Matrix = pingCrossProduct(a, peers)
	v.Total = len(v.Matrix)
	for _, r := range v.Matrix {
		if !r.OK {
			v.Failed++
		}
	}
	v.Pass = (v.Failed == 0)

	// In regression mode, the EXPECTED outcome is failure. We invert:
	// "the harness verdict is healthy" means "PASS in symmetric mode OR
	// FAIL in regression mode." run-regression.sh checks the verdict
	// and applies the inversion at shell level so the JSON itself
	// always reports the literal matrix outcome.

	v.FinishedAt = time.Now()
	if err := emitVerdict(a.JSONOut, *v); err != nil {
		return fmt.Errorf("emit verdict: %w", err)
	}
	// Exit code decision moves to runHarness AFTER deferred container +
	// network cleanup fires.
	return nil
}

// ----------------------------------------------------------------------
// Phase 1: image build.
// ----------------------------------------------------------------------

func ensureImage(a harnessArgs) error {
	// Check if the image already exists locally.
	if out, err := dockerCmd(a, "image", "inspect", a.Image).Output(); err == nil && len(out) > 0 {
		logf(a, "image %s already present, skipping build", a.Image)
		return nil
	}
	// Build it. The Dockerfile + entrypoint.sh sit alongside this binary's
	// source tree (../../ from cmd/harness/), but for portability we
	// resolve the Dockerfile path relative to a known anchor: the
	// directory of the bundle-create binary's source (../enrollment/...)
	// — we know we're somewhere inside the repo because we just compiled
	// from it. The harness's Dockerfile is two levels up from cmd/harness.
	dfDir, err := harnessSrcDir()
	if err != nil {
		return err
	}
	logf(a, "building image %s from %s", a.Image, dfDir)
	cmd := dockerCmd(a, "build", "-t", a.Image, dfDir)
	cmd.Stdout = stderrIfVerbose(a)
	cmd.Stderr = stderrIfVerbose(a)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}
	return nil
}

// harnessSrcDir returns the path to the harness source directory
// (ops/wg-cp0-mesh-reach/). Resolved from $PWD; falls back to
// $WGCP0_MESH_REACH_SRC env override for unit-test invocation.
func harnessSrcDir() (string, error) {
	if p := os.Getenv("WGCP0_MESH_REACH_SRC"); p != "" {
		return p, nil
	}
	// Look for Dockerfile starting from cwd, walking up.
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "cmd", "harness")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("cannot locate harness source dir (Dockerfile + cmd/harness); set $WGCP0_MESH_REACH_SRC")
}

// ----------------------------------------------------------------------
// Phase 2: build the bundle-create + wg-cp0-bundle-apply binaries.
// ----------------------------------------------------------------------

func buildGoBinary(a harnessArgs, name, srcRelPath string) (string, error) {
	src := filepath.Join(srcRelPath)
	if !filepath.IsAbs(src) {
		// Resolve relative to harness src dir.
		root, err := harnessSrcDir()
		if err != nil {
			return "", err
		}
		src = filepath.Join(root, srcRelPath)
	}
	outPath := filepath.Join(a.WorkDir, name)
	logf(a, "building %s from %s", name, src)
	cmd := exec.Command("go", "build", "-o", outPath, ".")
	cmd.Dir = src
	// CGO defaults to enabled — bundle-create + wg-cp0-bundle-apply
	// pull in piv-go (libpcsclite cgo) transitively via the enrollment
	// module. CI installs libpcsclite-dev; macOS local builds use the
	// system framework. CGO_ENABLED=0 would break the build.
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, string(out))
	}
	return outPath, nil
}

// ----------------------------------------------------------------------
// Phase 3: dev CA generation.
// ----------------------------------------------------------------------

// genDevCA produces an age-encrypted ECDSA P-256 CA private key + the
// matching age identity file + the CA's PEM-encoded SPKI public key
// (consumed by wg-cp0-bundle-apply as --ca-pubkey). Returns paths plus a
// in-process signer for the regression mode re-signing.
func genDevCA(a harnessArgs) (caKeyPath, ageIDPath, caID string, caPubPEM []byte, signer ca.Signer, err error) {
	ident, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", "", nil, nil, err
	}
	ageIDPath = filepath.Join(a.WorkDir, "age-id.txt")
	if err := os.WriteFile(ageIDPath, []byte(ident.String()+"\n"), 0o600); err != nil {
		return "", "", "", nil, nil, err
	}
	caKeyPath = filepath.Join(a.WorkDir, "ca.key.age")
	pub, err := ca.GenerateAndWrite(caKeyPath, []age.Recipient{ident.Recipient()})
	if err != nil {
		return "", "", "", nil, nil, err
	}
	derPub, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", "", "", nil, nil, err
	}
	caPubPEM = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: derPub})
	caID = fmt.Sprintf("dev-mesh-reach-%d", a.Now.Unix())

	// In-process signer (for regression-mode re-signing). Build an
	// AgeSigner over the same key + identity.
	sgr, err := ca.NewAgeSigner(caID, caKeyPath, []age.Identity{ident})
	if err != nil {
		return "", "", "", nil, nil, err
	}
	signer = sgr
	return caKeyPath, ageIDPath, caID, caPubPEM, signer, nil
}

// kindCPRelay matches the string used by bundle-create at endpoint
// construction (bundle.Kind("cp-relay")). There is no exported constant
// for it in the bundle package; we mirror the same string here.
const kindCPRelay = bundle.Kind("cp-relay")

// ----------------------------------------------------------------------
// Phase 4: relay keypair generation + relays.json.
// ----------------------------------------------------------------------

type relayInfo struct {
	Name          string // relay-0, relay-1, ...
	Index         int
	WGPub         []byte // 32-byte Curve25519
	WGPriv        []byte // 32-byte Curve25519 (clamped)
	MeshAddr      string // 198.18.0.<2+i>
	MeshAddrCIDR  string // 198.18.0.<2+i>/24
	BridgeIP      string // 172.30.0.<10+i>
	Endpoint      string // <bridgeIP>:51821 (as bundle-create writes into KnownEndpoint.Addr)
	ContainerName string
	ConfPath      string // /work/relay-i/wg-cp0.conf (host path)
}

type peerInfo struct {
	Name          string // peer-0, peer-1, ...
	Index         int
	WGPubB64      string
	MeshAddr      string // 198.18.0.<10+i>
	MeshAddrCIDR  string // 198.18.0.<10+i>/24
	BridgeIP      string // 172.30.0.<100+i>
	BundlePath    string
	ConfPath      string
	PrivkeyPath   string
	ContainerName string
}

func genRelays(a harnessArgs) ([]relayInfo, error) {
	rs := make([]relayInfo, a.M)
	for i := 0; i < a.M; i++ {
		priv, err := generateClamped()
		if err != nil {
			return nil, fmt.Errorf("relay %d keygen: %w", i, err)
		}
		pub, err := curve25519.X25519(priv, curve25519.Basepoint)
		if err != nil {
			return nil, fmt.Errorf("relay %d derive pub: %w", i, err)
		}
		mesh := fmt.Sprintf("198.18.0.%d", relayFirstHostByte+i)
		bridge := fmt.Sprintf("172.30.0.%d", relayBridgeBaseByte+i)
		rs[i] = relayInfo{
			Name:          fmt.Sprintf("relay-%d", i),
			Index:         i,
			WGPub:         pub,
			WGPriv:        priv,
			MeshAddr:      mesh,
			MeshAddrCIDR:  mesh + "/24",
			BridgeIP:      bridge,
			Endpoint:      fmt.Sprintf("%s:%d", bridge, wgListenPort),
			ContainerName: fmt.Sprintf("wgcp0-bench-relay-%d", i),
			ConfPath:      filepath.Join(a.WorkDir, fmt.Sprintf("relay-%d", i), "wg-cp0.conf"),
		}
	}
	return rs, nil
}

// generateClamped produces a Curve25519 private scalar with the standard
// clamping (top 3 bits cleared, top byte's MSB set, bottom 3 bits cleared)
// per RFC 7748 §5. Matches the canonical wg-cp0 keygen path.
func generateClamped() ([]byte, error) {
	k := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return nil, err
	}
	k[0] &= 248
	k[31] &= 127
	k[31] |= 64
	return k, nil
}

type relayJSONEntry struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Address  string `json:"address"`
	Pubkey   string `json:"pubkey"`
}

func writeRelaysJSON(path string, relays []relayInfo) error {
	out := make([]relayJSONEntry, len(relays))
	for i, r := range relays {
		out[i] = relayJSONEntry{
			Name:     r.Name,
			Endpoint: r.Endpoint,
			Address:  r.MeshAddr,
			Pubkey:   base64.StdEncoding.EncodeToString(r.WGPub),
		}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ----------------------------------------------------------------------
// Phase 5: mint bundles via the real bundle-create binary.
// ----------------------------------------------------------------------

func mintBundles(a harnessArgs, relaysPath, caKeyPath, ageIDPath, caID string) ([]peerInfo, error) {
	peers := make([]peerInfo, a.N)
	for i := 0; i < a.N; i++ {
		mesh := fmt.Sprintf("198.18.0.%d", peerFirstHostByte+i)
		bridge := fmt.Sprintf("172.30.0.%d", peerBridgeBaseByte+i)
		name := fmt.Sprintf("peer-%d", i)
		peerDir := filepath.Join(a.WorkDir, name)
		if err := os.MkdirAll(peerDir, 0o700); err != nil {
			return nil, err
		}
		bundlePath := filepath.Join(peerDir, "bundle.cbor")
		args := []string{
			"--site", "bench",
			"--device-id", name,
			"--wg-cp0-address", mesh + "/24",
			"--wg-cp0-relays", relaysPath,
			"--acl-groups", "BENCH",
			"--ca-id", caID,
			"--mode", "dev",
			"--ca-key", caKeyPath,
			"--age-identity", ageIDPath,
			"--out", bundlePath,
		}
		logf(a, "minting bundle for %s -> %s", name, bundlePath)
		cmd := exec.Command(a.BundleCreatePath, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("%s: %w: %s", name, err, string(out))
		}
		peers[i] = peerInfo{
			Name:          name,
			Index:         i,
			MeshAddr:      mesh,
			MeshAddrCIDR:  mesh + "/24",
			BridgeIP:      bridge,
			BundlePath:    bundlePath,
			ConfPath:      filepath.Join(peerDir, "wg-cp0.conf"),
			PrivkeyPath:   filepath.Join(peerDir, "wg-cp0-priv.key"),
			ContainerName: fmt.Sprintf("wgcp0-bench-peer-%d", i),
		}
	}
	return peers, nil
}

// ----------------------------------------------------------------------
// Phase 6: regression-mode bundle drift.
// ----------------------------------------------------------------------

// driftBundlesAsymmetric mutates each peer's bundle to emulate the
// pre-ADR-0321 placement: catchall `198.18.0.0/24` placed on a SINGLE
// relay (a different one per peer), other relay peer blocks carry only
// MeshAddr/32. Then re-signs with the dev CA.
//
// Per peer i, the winning relay is i % M — so for N=4, M=3, peer-0's
// catchall is on relay-0, peer-1's on relay-1, peer-2's on relay-2,
// peer-3 wraps back to relay-0. This guarantees at least two peers
// disagree, which is the F-116 failure mode.
func driftBundlesAsymmetric(a harnessArgs, peers []peerInfo, signer ca.Signer) error {
	for i, p := range peers {
		raw, err := os.ReadFile(p.BundlePath)
		if err != nil {
			return err
		}
		b, err := bundle.Unmarshal(raw)
		if err != nil {
			return err
		}
		winner := i % a.M
		for j := range b.KnownEndpoints {
			if b.KnownEndpoints[j].Kind != kindCPRelay {
				continue
			}
			mesh := b.KnownEndpoints[j].MeshAddr
			if j == winner {
				b.KnownEndpoints[j].AllowedIPs = []string{mesh + "/32", fleetSubnetCIDR}
			} else {
				// Pre-ADR-0321 default: MeshAddr/32 only on
				// non-winning relays.
				b.KnownEndpoints[j].AllowedIPs = []string{mesh + "/32"}
			}
		}
		// Re-sign.
		if err := b.Sign(signer.Sign); err != nil {
			return fmt.Errorf("re-sign %s: %w", p.Name, err)
		}
		out, err := b.Marshal()
		if err != nil {
			return err
		}
		if err := os.WriteFile(p.BundlePath, out, 0o600); err != nil {
			return err
		}
		logf(a, "regression: drifted %s — catchall on relay-%d only", p.Name, winner)
	}
	return nil
}

// readBundle is a thin wrapper for Unmarshal that handles the file read.
func readBundle(path string) (*bundle.EnrollmentBundle, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return bundle.Unmarshal(raw)
}

// ----------------------------------------------------------------------
// Phase 8: render peer wg-cp0.conf via wg-cp0-bundle-apply.
// ----------------------------------------------------------------------

func renderPeerConf(a harnessArgs, p *peerInfo, caPubPEM []byte) error {
	caPubPath := filepath.Join(filepath.Dir(p.BundlePath), "ca-pubkey.pem")
	if err := os.WriteFile(caPubPath, caPubPEM, 0o600); err != nil {
		return err
	}
	args := []string{
		"--in", p.BundlePath,
		"--ca-pubkey", caPubPath,
		"--out-conf", p.ConfPath,
		"--out-privkey", p.PrivkeyPath,
	}
	cmd := exec.Command(a.BundleApplyPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	logf(a, "rendered %s -> %s", p.Name, p.ConfPath)
	return nil
}

// ----------------------------------------------------------------------
// Phase 9: render relay-side wg-cp0.conf.
// ----------------------------------------------------------------------

func renderRelayConf(a harnessArgs, r *relayInfo, peers []peerInfo) error {
	dir := filepath.Dir(r.ConfPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Rendered by wg-cp0-mesh-reach harness for %s.\n", r.Name)
	fmt.Fprintf(&sb, "# Relay-side config — every peer listed with AllowedIPs = peer.MeshAddr/32.\n")
	fmt.Fprintf(&sb, "# Symmetric by construction on the relay side per ops/ansible/roles/wg-cp0-server/.\n\n")
	fmt.Fprintf(&sb, "[Interface]\n")
	fmt.Fprintf(&sb, "PrivateKey = %s\n", base64.StdEncoding.EncodeToString(r.WGPriv))
	fmt.Fprintf(&sb, "Address    = %s\n", r.MeshAddrCIDR)
	fmt.Fprintf(&sb, "ListenPort = %d\n", wgListenPort)
	for _, p := range peers {
		fmt.Fprintf(&sb, "\n[Peer]\n# %s\n", p.Name)
		fmt.Fprintf(&sb, "PublicKey  = %s\n", p.WGPubB64)
		fmt.Fprintf(&sb, "AllowedIPs = %s/32\n", p.MeshAddr)
	}
	if err := os.WriteFile(r.ConfPath, []byte(sb.String()), 0o600); err != nil {
		return err
	}
	logf(a, "rendered %s -> %s (%d peers)", r.Name, r.ConfPath, len(peers))
	return nil
}

// ----------------------------------------------------------------------
// Phase 10: docker run.
// ----------------------------------------------------------------------

func dockerNetworkCreate(a harnessArgs) error {
	// Tear down any leftover from a previous interrupted run.
	dockerCmd(a, "network", "rm", a.NetworkName).Run() // best-effort
	cmd := dockerCmd(a, "network", "create",
		"--driver", "bridge",
		"--subnet", bridgeNetworkCIDR,
		a.NetworkName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

func dockerNetworkRemove(a harnessArgs) {
	dockerCmd(a, "network", "rm", a.NetworkName).Run()
}

func dockerStopRemove(a harnessArgs, name string) {
	dockerCmd(a, "rm", "-f", name).Run()
}

func dockerRunRelay(a harnessArgs, r *relayInfo) error {
	// Mount the work-dir read-only at /work; the entrypoint copies the
	// per-container config into /etc/wireguard/.
	args := []string{
		"run", "-d",
		"--name", r.ContainerName,
		"--network", a.NetworkName,
		"--ip", r.BridgeIP,
		"--cap-add", "NET_ADMIN",
		"--sysctl", "net.ipv4.ip_forward=1",
		"-v", a.WorkDir + ":/work:ro",
		"-e", "ROLE=relay",
		"-e", "NAME=" + r.Name,
		"-p", fmt.Sprintf("%d/udp", wgListenPort), // not strictly needed since peers reach us via bridge IP, but harmless
		a.Image,
	}
	// Drop --p since the bridge already connects them; avoids host-port
	// conflicts when M relays all try to publish 51821.
	args = stripPublishFlag(args)
	cmd := dockerCmd(a, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	logf(a, "started %s (%s)", r.ContainerName, r.BridgeIP)
	return nil
}

func dockerRunPeer(a harnessArgs, p *peerInfo) error {
	args := []string{
		"run", "-d",
		"--name", p.ContainerName,
		"--network", a.NetworkName,
		"--ip", p.BridgeIP,
		"--cap-add", "NET_ADMIN",
		"-v", a.WorkDir + ":/work:ro",
		"-e", "ROLE=peer",
		"-e", "NAME=" + p.Name,
		a.Image,
	}
	cmd := dockerCmd(a, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	logf(a, "started %s (%s)", p.ContainerName, p.BridgeIP)
	return nil
}

// stripPublishFlag removes any `-p ...` pair from the args slice. Used to
// suppress host-port publishing on relays when we don't need it.
func stripPublishFlag(args []string) []string {
	out := make([]string, 0, len(args))
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if a == "-p" {
			skip = true
			continue
		}
		out = append(out, a)
	}
	return out
}

// waitContainersReady polls each container until its wg-cp0 interface is
// reported up, or a timeout fires. The entrypoint prints "ready" after
// wg-quick up; we poll for that as a cheap readiness signal.
func waitContainersReady(a harnessArgs, peers []peerInfo, relays []relayInfo) error {
	deadline := time.Now().Add(30 * time.Second)
	containers := append([]string{}, namesOf(relays)...)
	for _, p := range peers {
		containers = append(containers, p.ContainerName)
	}
	for time.Now().Before(deadline) {
		allReady := true
		for _, c := range containers {
			out, _ := dockerCmd(a, "exec", c, "wg", "show", "wg-cp0").Output()
			if !strings.Contains(string(out), "interface:") {
				allReady = false
				break
			}
		}
		if allReady {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for wg-cp0 to come up on all containers")
}

func namesOf(rs []relayInfo) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ContainerName
	}
	return out
}

// ----------------------------------------------------------------------
// Phase 11: cross-product ping.
// ----------------------------------------------------------------------

func pingCrossProduct(a harnessArgs, peers []peerInfo) []matrixResult {
	// Run pings in parallel — N*(N-1) probes, each ~1s. Concurrency
	// bounded to 8 to keep docker exec contention bounded.
	type job struct {
		src, dst peerInfo
	}
	var jobs []job
	for _, s := range peers {
		for _, d := range peers {
			if s.Name == d.Name {
				continue
			}
			jobs = append(jobs, job{s, d})
		}
	}
	results := make([]matrixResult, len(jobs))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, j := range jobs {
		i, j := i, j
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out, err := dockerCmd(a,
				"exec", j.src.ContainerName,
				"ping", "-c", fmt.Sprint(defaultPingCount),
				"-W", fmt.Sprint(defaultPingTimeout),
				j.dst.MeshAddr,
			).CombinedOutput()
			results[i] = matrixResult{
				Src:   j.src.Name,
				Dst:   j.dst.Name,
				DstIP: j.dst.MeshAddr,
				OK:    err == nil,
			}
			if err != nil {
				results[i].Output = strings.TrimSpace(string(out))
			}
		}()
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool {
		if results[i].Src != results[j].Src {
			return results[i].Src < results[j].Src
		}
		return results[i].Dst < results[j].Dst
	})
	return results
}

// ----------------------------------------------------------------------
// Output.
// ----------------------------------------------------------------------

func emitVerdict(path string, v verdict) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ----------------------------------------------------------------------
// Helpers.
// ----------------------------------------------------------------------

func dockerCmd(a harnessArgs, args ...string) *exec.Cmd {
	// Deliberately leave Stdout/Stderr unset. Callers that want output
	// streamed should attach writers themselves AFTER this call; most
	// callers use CombinedOutput() / Output() which require nil pipes.
	return exec.Command("docker", args...)
}

func stderrIfVerbose(a harnessArgs) io.Writer {
	if a.Verbose {
		return os.Stderr
	}
	return io.Discard
}

func logf(a harnessArgs, format string, args ...any) {
	if a.Verbose {
		fmt.Fprintf(os.Stderr, "[harness] "+format+"\n", args...)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "harness: "+format+"\n", args...)
	os.Exit(2)
}

// Compile-time sanity check: rand.Reader is used by generateClamped.
var _ = rand.Reader
