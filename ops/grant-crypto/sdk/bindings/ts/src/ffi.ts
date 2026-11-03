/**
 * The thin FFI seam: load `libgrant_crypto_capi` and declare its exported C ABI
 * symbols (see `capi/include/grant_crypto.h`). Nothing above this file touches koffi
 * or raw pointers.
 *
 * Dependency pin — koffi is pinned to the EXACT version `2.16.3` (see package.json).
 * koffi's license history matters here: it was LGPL through 2.3.8 and relicensed to
 * MIT at 2.3.9; we require a version at or after that boundary so the binding carries
 * no copyleft obligation. 2.16.3 is the last release of the mature 2.x line (3.x is a
 * ground-up rewrite with a different API surface), so it is the conservative,
 * community-grade choice: MIT, battle-tested, and API-stable against the calls below.
 */

import * as fs from "node:fs";
import * as path from "node:path";
import koffi from "koffi";

/** The platform-specific shared-library file name for the capi cdylib. */
function libFileName(): string {
  switch (process.platform) {
    case "darwin":
      return "libgrant_crypto_capi.dylib";
    case "win32":
      return "grant_crypto_capi.dll";
    default:
      return "libgrant_crypto_capi.so";
  }
}

/**
 * Resolve the capi shared library. Order:
 *   1. `GRANT_CRYPTO_CAPI_LIB` — the library file itself, OR a directory containing
 *      it (same file-or-dir semantics as the Python binding's loader).
 *   2. A relative dev-path search: walk up from this module and try
 *      `<dir>/target/{debug,release}/<lib>` at each level, matching the in-repo
 *      Cargo layout (`ops/grant-crypto/sdk/target/...`).
 * Throws a crisp, actionable error listing every path tried if none exists.
 */
function resolveLibPath(): string {
  const override = process.env.GRANT_CRYPTO_CAPI_LIB;
  if (override) {
    if (fs.existsSync(override)) {
      if (fs.statSync(override).isDirectory()) {
        const inDir = path.join(override, libFileName());
        if (fs.existsSync(inDir)) return inDir;
        throw new Error(
          `grant-crypto: GRANT_CRYPTO_CAPI_LIB is the directory ${override}, but ${libFileName()} is not in it.`,
        );
      }
      return override;
    }
    throw new Error(
      `grant-crypto: GRANT_CRYPTO_CAPI_LIB is set to ${override}, but no file exists there.`,
    );
  }

  const name = libFileName();
  const tried: string[] = [];
  let dir = __dirname;
  // 8 levels comfortably covers dist/src -> package root -> sdk (where target/ lives).
  for (let i = 0; i < 8; i++) {
    for (const profile of ["debug", "release"]) {
      const candidate = path.join(dir, "target", profile, name);
      tried.push(candidate);
      if (fs.existsSync(candidate)) return candidate;
    }
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }

  throw new Error(
    `grant-crypto: could not find ${name}. Build the C ABI first:\n` +
      `  (cd ops/grant-crypto/sdk && cargo build -p grant-crypto-capi)\n` +
      `or set GRANT_CRYPTO_CAPI_LIB to its full path.\nTried:\n  ${tried.join("\n  ")}`,
  );
}

const lib = koffi.load(resolveLibPath());

// C ABI symbol table. Signatures mirror grant_crypto.h verbatim; `size_t` stands in
// for the header's `uintptr_t` (identical width on every supported platform). Buffer
// arguments are passed as Node Buffers/Uint8Arrays; `_Out_ T*` params take a one-element
// JS array that koffi fills. Handles are u64 ids (small, monotone) — safe as JS numbers.
export const gc = {
  abi_version: lib.func("uint32_t gc_abi_version()"),
  error_message: lib.func("const char* gc_error_message(int32_t code)"),

  identity_new: lib.func(
    "int32_t gc_identity_new(const char* member_id, const uint8_t* scalar32, _Out_ uint64_t* out)",
  ),
  identity_dispose: lib.func("int32_t gc_identity_dispose(uint64_t h)"),

  keystore_new: lib.func("int32_t gc_keystore_new(_Out_ uint64_t* out)"),
  keystore_dispose: lib.func("int32_t gc_keystore_dispose(uint64_t h)"),
  keystore_admit: lib.func(
    "int32_t gc_keystore_admit(uint64_t ks, uint64_t id, const uint8_t* bundle_json, size_t len, _Out_ bool* out_had_slot)",
  ),
  keystore_open: lib.func(
    "int32_t gc_keystore_open(uint64_t ks, const char* keyexpr, const uint8_t* envelope_json, size_t len, _Out_ uint64_t* out_buf)",
  ),

  streamkey_acquire: lib.func(
    "int32_t gc_streamkey_acquire(const uint8_t* bundles_json, size_t len, const char* rs, uint64_t id, _Out_ uint64_t* out)",
  ),
  streamkey_set_cap: lib.func("int32_t gc_streamkey_set_cap(uint64_t sk, uint64_t cap)"),
  streamkey_remaining: lib.func("int32_t gc_streamkey_remaining(uint64_t sk, _Out_ uint64_t* out)"),
  streamkey_dispose: lib.func("int32_t gc_streamkey_dispose(uint64_t h)"),
  streamkey_seal: lib.func(
    "int32_t gc_streamkey_seal(uint64_t sk, const char* keyexpr, const uint8_t* plaintext, size_t len, _Out_ uint64_t* out_buf)",
  ),

  rotate: lib.func(
    "int32_t gc_rotate(uint64_t identity, const uint8_t* new_scalar32, bool keep_access, const uint8_t* bundles_json, size_t len, _Out_ uint64_t* out_receipt_json)",
  ),

  verify_bundle: lib.func(
    "int32_t gc_verify_bundle(const uint8_t* bundle_json, size_t bundle_len, const uint8_t* vk_spki_der, size_t vk_len)",
  ),

  buf_ptr: lib.func("uint8_t* gc_buf_ptr(uint64_t h)"),
  buf_len: lib.func("size_t gc_buf_len(uint64_t h)"),
  buf_dispose: lib.func("int32_t gc_buf_dispose(uint64_t h)"),
};

/** The koffi handle for the shared library (exposed for diagnostics/tests only). */
export { koffi };
