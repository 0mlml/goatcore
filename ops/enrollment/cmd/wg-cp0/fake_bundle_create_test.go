package main

// fake_bundle_create_test.go — once-per-test-process build of the
// test-only bundle-create stand-in under testdata/fakebundlecreate.
// Tests call ensureFakeBundleCreate(t) which compiles the fake (or
// returns the cached path) and sets WG_CP0_BUNDLE_CREATE_BIN +
// GOAT_CA_* env vars so the apply path runs end-to-end.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

var (
	fakeBinOnce sync.Once
	fakeBinPath string
	fakeBinErr  error
)

// ensureFakeBundleCreate builds the test-only bundle-create stand-in
// once per test process and returns its path. Subsequent callers
// reuse the cached binary. The test also sets WG_CP0_BUNDLE_CREATE_BIN
// + GOAT_CA_* env vars via t.Setenv so the apply path picks them up
// without leaking into sibling tests.
func ensureFakeBundleCreate(t *testing.T) string {
	t.Helper()
	fakeBinOnce.Do(func() {
		// Locate the testdata directory relative to this test file.
		// `go test` runs in the package directory, so testdata/ is a
		// stable relative path. runtime.Caller gives the absolute
		// form, which makes the `go build` invocation reproducible
		// regardless of the parent test's working directory.
		_, here, _, _ := runtime.Caller(0)
		pkgDir := filepath.Dir(here)
		src := filepath.Join(pkgDir, "testdata", "fakebundlecreate")

		// Build the fake into the OS temp dir so the binary survives
		// across t.Cleanup boundaries for the test process lifetime
		// (sync.Once + os.TempDir keeps a single binary cached).
		outDir, err := os.MkdirTemp("", "fakebundlecreate-")
		if err != nil {
			fakeBinErr = err
			return
		}
		outBin := filepath.Join(outDir, "fakebundlecreate")
		buildCtx, buildCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer buildCancel()
		// #nosec G204 — test-only `go build` for the fake bundle-
		// create stand-in under testdata/. outBin is a literal
		// test-temp path; src is a literal repo-relative path. No
		// untrusted input reaches argv.
		cmd := exec.CommandContext(buildCtx, "go", "build", "-o", outBin, ".")
		cmd.Dir = src
		if output, err := cmd.CombinedOutput(); err != nil {
			fakeBinErr = err
			t.Logf("fakebundlecreate build output:\n%s", string(output))
			return
		}
		fakeBinPath = outBin
	})
	if fakeBinErr != nil {
		t.Fatalf("ensureFakeBundleCreate: %v", fakeBinErr)
	}
	return fakeBinPath
}

// withFakeBundleCreateEnv sets WG_CP0_BUNDLE_CREATE_BIN + the
// signing-material env vars so an apply-path test can exec the fake
// without any further plumbing. Calls t.Setenv for automatic
// scoping; safe to call multiple times.
func withFakeBundleCreateEnv(t *testing.T) string {
	t.Helper()
	bin := ensureFakeBundleCreate(t)
	t.Setenv("WG_CP0_BUNDLE_CREATE_BIN", bin)
	t.Setenv("GOAT_CA_MODE", "dev")
	t.Setenv("GOAT_CA_ID", "test-ca-2026")
	t.Setenv("GOAT_CA_KEY", "/dev/null")
	t.Setenv("GOAT_AGE_IDENTITY", "/dev/null")
	t.Setenv("GOAT_TLS_TRUST_ANCHOR", "")
	return bin
}
