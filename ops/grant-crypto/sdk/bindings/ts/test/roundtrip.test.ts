/**
 * Write-side coverage (vectors can't cover the deliberately-nondeterministic write
 * path, ADR 1106): a producer acquires a stream key, seals, and a consumer opens it;
 * the enforced seal cap; and both rotate policies. All driven off the committed
 * `base` vector — its `member` has a slot in the signed bundle, so it can act as both
 * producer (acquire) and consumer (admit/open).
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  Identity,
  KeyStore,
  StreamKey,
  abiVersion,
  RotationRequiredError,
  UnwrapError,
  MalformedError,
  type RotationReceipt,
} from "../src/index";
import { loadVectors, jsonBytes, hexToBytes, freshP256Scalar, enc, dec } from "./vectors";

const vectors = loadVectors();
const RS = "coalition-air";
const MEMBER = "member";

function memberScalar(): Uint8Array {
  const hex = vectors.keys[MEMBER]?.priv_scalar_hex;
  assert.ok(hex, "member scalar present");
  return hexToBytes(hex);
}
function baseBundle(): unknown {
  const art = vectors.artifacts["base"];
  assert.ok(art, "base artifact present");
  return art.bundle;
}
function bundlesJson(): Uint8Array {
  return jsonBytes([baseBundle()]);
}
function bundleJson(): Uint8Array {
  return jsonBytes(baseBundle());
}

test("abiVersion is the pinned lockstep version", () => {
  assert.equal(abiVersion(), 1);
});

test("producer seals and a member opens (write → read roundtrip)", () => {
  const keyexpr = vectors.artifacts["base"]!.keyexpr;
  const plaintext = enc.encode("brigade COP snapshot, REL-EXAMPLE only");

  const id = Identity.create(MEMBER, memberScalar());
  const sk = StreamKey.acquire(bundlesJson(), RS, id);
  const envelope = sk.seal(keyexpr, plaintext);
  assert.ok(envelope.length > 0, "sealed envelope is non-empty");

  const ks = KeyStore.create();
  const hadSlot = ks.admit(id, bundleJson());
  assert.equal(hadSlot, true, "member has a slot");
  const opened = ks.open(keyexpr, envelope);
  assert.deepEqual(opened, plaintext, "roundtrip plaintext matches");

  ks.close();
  sk.close();
  id.close();
});

test("seal cap: tighten-only, counts down, refuses at the cap", () => {
  const keyexpr = vectors.artifacts["base"]!.keyexpr;
  const id = Identity.create(MEMBER, memberScalar());
  const sk = StreamKey.acquire(bundlesJson(), RS, id);

  // Cannot loosen above the built-in default cap.
  assert.throws(() => sk.setCap(Number.MAX_SAFE_INTEGER), MalformedError);

  // The seal budget is process-cumulative per (rs, gen) and the bindings share the one
  // committed generation, so tighten relative to what remains now to leave exactly two.
  const DEFAULT_CAP = 1 << 24;
  const already = DEFAULT_CAP - Number(sk.remaining());
  sk.setCap(already + 2);
  assert.equal(sk.remaining(), 2);
  sk.seal(keyexpr, enc.encode("m1"));
  assert.equal(sk.remaining(), 1);
  sk.seal(keyexpr, enc.encode("m2"));
  assert.equal(sk.remaining(), 0);
  assert.throws(() => sk.seal(keyexpr, enc.encode("m3")), RotationRequiredError);

  sk.close();
  id.close();
});

test("each seal uses a fresh nonce (8 distinct)", () => {
  const keyexpr = vectors.artifacts["base"]!.keyexpr;
  const id = Identity.create(MEMBER, memberScalar());
  const sk = StreamKey.acquire(bundlesJson(), RS, id);

  const nonces = new Set<string>();
  for (let i = 0; i < 8; i++) {
    const env = JSON.parse(dec.decode(sk.seal(keyexpr, enc.encode("same plaintext")))) as {
      nonce: string;
    };
    nonces.add(env.nonce);
  }
  assert.equal(nonces.size, 8, "each seal must carry a distinct nonce");

  sk.close();
  id.close();
});

test("rotate Destroy: loses access, receipt is a self-dating fact", () => {
  const id = Identity.create(MEMBER, memberScalar());
  const receipt: RotationReceipt = id.rotate(freshP256Scalar(), false, bundlesJson());

  assert.equal(receipt.policy, "destroy");
  assert.equal(receipt.now_unreadable_generations, 1);
  assert.equal(receipt.rewrapped_generations, 0);
  assert.equal(receipt.rewrapped, undefined, "destroy carries no rewrapped entries");
  assert.match(receipt.timestamp_rfc3339, /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/);

  // The handle now holds the NEW key: the old slot (wrapped to the old key) no longer unwraps.
  const ks = KeyStore.create();
  assert.throws(() => ks.admit(id, bundleJson()), UnwrapError);
  ks.close();
  id.close();
});

test("rotate KeepAccess: re-wraps live DEKs so the new key opens them", () => {
  const id = Identity.create(MEMBER, memberScalar());
  const receipt: RotationReceipt = id.rotate(freshP256Scalar(), true, bundlesJson());

  assert.equal(receipt.policy, "keep-access");
  assert.equal(receipt.now_unreadable_generations, 1);
  assert.equal(receipt.rewrapped_generations, 1);
  assert.ok(receipt.rewrapped && receipt.rewrapped.length === 1, "one re-wrapped generation");
  const rw = receipt.rewrapped[0]!;
  assert.equal(rw.rs, RS);
  assert.equal(rw.gen, 7);

  // Reconstruct a bundle from the re-wrapped slot; the now-new identity must unwrap it.
  const rewrappedBundle = {
    suite: "v1-ecdh-p256",
    rs: RS,
    gen: 7,
    members: { [MEMBER]: rw.wrapped },
    signer: "",
    sig: "",
  };
  const ks = KeyStore.create();
  const hadSlot = ks.admit(id, enc.encode(JSON.stringify(rewrappedBundle)));
  assert.equal(hadSlot, true, "the new key unwraps the re-wrapped slot");
  ks.close();
  id.close();
});
