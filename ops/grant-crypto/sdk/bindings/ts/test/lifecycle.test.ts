/**
 * Handle-lifecycle coverage: fail-closed use-after-close, harmless double-close, input
 * validation, and `Symbol.dispose` (`using`) idempotence. The ABI itself fail-closes
 * (never dereferences freed state); the binding surfaces that as {@link DisposedHandleError}.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { Identity, KeyStore, StreamKey, DisposedHandleError, MalformedError } from "../src/index";
import { loadVectors, hexToBytes, jsonBytes } from "./vectors";

const vectors = loadVectors();
const MEMBER = "member";

function memberScalar(): Uint8Array {
  return hexToBytes(vectors.keys[MEMBER]!.priv_scalar_hex!);
}

test("use-after-close throws DisposedHandleError (keystore)", () => {
  const id = Identity.create(MEMBER, memberScalar());
  const ks = KeyStore.create();
  ks.close();
  assert.throws(() => ks.admit(id, jsonBytes(vectors.artifacts["base"]!.bundle)), DisposedHandleError);
  assert.throws(() => ks.open("data/x", new Uint8Array([1, 2, 3])), DisposedHandleError);
  id.close();
});

test("double-close throws DisposedHandleError (harmless)", () => {
  const ks = KeyStore.create();
  ks.close();
  assert.throws(() => ks.close(), DisposedHandleError);

  const id = Identity.create(MEMBER, memberScalar());
  id.close();
  assert.throws(() => id.close(), DisposedHandleError);
});

test("Identity.create rejects a wrong-length or invalid scalar with MalformedError", () => {
  assert.throws(() => Identity.create(MEMBER, new Uint8Array(31)), MalformedError);
  // A 32-byte all-zero scalar is not a valid P-256 key: the ABI rejects it.
  assert.throws(() => Identity.create(MEMBER, new Uint8Array(32)), MalformedError);
});

test("Symbol.dispose (using) closes exactly once, no throw on scope exit", () => {
  let leaked: KeyStore;
  {
    using ks = KeyStore.create();
    leaked = ks; // capture to prove it is closed after the block
  }
  // After the `using` block the handle is disposed; a further call fail-closes.
  assert.throws(() => leaked.open("data/x", new Uint8Array([1])), DisposedHandleError);
});

test("embedded NUL in a string argument throws MalformedError", () => {
  // The C ABI reads string args NUL-terminated, so an interior NUL would silently
  // truncate. The binding rejects it client-side before the FFI call.
  const bundles = jsonBytes([vectors.artifacts["base"]!.bundle]);
  assert.throws(() => Identity.create("member\u0000evil", memberScalar()), MalformedError);

  const id = Identity.create(MEMBER, memberScalar());
  assert.throws(() => StreamKey.acquire(bundles, "coalition\u0000air", id), MalformedError);

  const sk = StreamKey.acquire(bundles, "coalition-air", id);
  assert.throws(() => sk.seal("data/x\u0000y", new Uint8Array([1])), MalformedError);

  const ks = KeyStore.create();
  assert.throws(() => ks.open("data/x\u0000y", new Uint8Array([1])), MalformedError);

  ks.close();
  sk.close();
  id.close();
});

test("StreamKey acquire fails closed for a non-member (NoSlot)", () => {
  // A fresh identity id that is not in the base bundle's audience.
  const outsiderHex = vectors.keys["outsider"]!.priv_scalar_hex!;
  const id = Identity.create("outsider", hexToBytes(outsiderHex));
  assert.throws(
    () => StreamKey.acquire(jsonBytes([vectors.artifacts["base"]!.bundle]), "coalition-air", id),
    (e: unknown) => e instanceof Error && (e as { code?: number }).code === -5,
  );
  id.close();
});
