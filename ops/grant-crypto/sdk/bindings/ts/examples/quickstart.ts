/**
 * Runnable, self-verifying tour of the grant-crypto TypeScript binding.
 *
 * Walks the whole surface once — identity -> acquire -> seal -> verify -> admit ->
 * open -> rotate — asserts the roundtrip, prints the rotation receipt, and exits
 * non-zero if anything fails (so CI can run it as a smoke test).
 *
 * Build the C ABI once and run it:
 *
 *     cd ops/grant-crypto/sdk && cargo build -p grant-crypto-capi
 *     cd bindings/ts && npm install && npm run example
 *
 * The signed KeyBundle and key material come from the committed cross-language
 * conformance vectors (the same material every binding validates against) — this
 * example does not re-implement the key service. The vectors' `member` identity has a
 * slot in the `base` bundle, so it acts as both producer (acquire/seal) and consumer
 * (admit/open) here.
 */

import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as path from "node:path";
import * as crypto from "node:crypto";

import {
  Identity,
  KeyStore,
  StreamKey,
  verifyBundle,
  DecryptError,
  type RotationReceipt,
} from "../src/index";

const RELEASE_SET = "coalition-air";
const KEYEXPR = "data/coalition-air/cop";

/** Walk up from here until the committed conformance vectors are found. */
function loadVectors(): {
  keys: Record<string, { priv_scalar_hex?: string; pub_spki_b64?: string }>;
  artifacts: Record<string, { bundle: unknown; keyexpr: string }>;
} {
  let dir = __dirname;
  for (let i = 0; i < 12; i++) {
    const p = path.join(dir, "envelope", "testdata", "vectors", "vectors.json");
    if (fs.existsSync(p)) return JSON.parse(fs.readFileSync(p, "utf8"));
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  throw new Error("could not locate envelope/testdata/vectors/vectors.json");
}

/** A fresh, valid raw 32-byte P-256 scalar to rotate into. */
function freshScalar(): Uint8Array {
  const { privateKey } = crypto.generateKeyPairSync("ec", { namedCurve: "P-256" });
  const jwk = privateKey.export({ format: "jwk" }) as { d?: string };
  if (!jwk.d) throw new Error("no private scalar in generated JWK");
  const raw = Buffer.from(jwk.d, "base64url");
  const out = new Uint8Array(32);
  out.set(raw, 32 - raw.length); // left-pad if a leading zero was dropped
  return out;
}

function main(): void {
  const v = loadVectors();
  const enc = new TextEncoder();
  const dec = new TextDecoder();

  const memberScalar = Uint8Array.from(Buffer.from(v.keys["member"]!.priv_scalar_hex!, "hex"));
  const keyServiceSpki = Uint8Array.from(Buffer.from(v.keys["keyservice"]!.pub_spki_b64!, "base64"));
  const bundle = v.artifacts["base"]!.bundle;
  const bundleJson = enc.encode(JSON.stringify(bundle));
  const bundlesJson = enc.encode(JSON.stringify([bundle]));

  const message = enc.encode("brigade COP snapshot");

  const id = Identity.create("member", memberScalar);
  const sk = StreamKey.acquire(bundlesJson, RELEASE_SET, id);
  const ks = KeyStore.create();
  try {
    // --- Producer: acquire a seal-capable key from our own slot, then seal. ---
    // seal takes no recipients: the key service already resolved the audience.
    sk.setCap(2); // tighten-only, for a legible remaining() below
    const envelope = sk.seal(KEYEXPR, message);
    const remaining = sk.remaining();
    assert.equal(remaining, 1, "remaining after one seal");

    // --- Consumer: verify the bundle, admit our slot, open the message. ---
    // Consumers MUST verify before admitting; a bad signature throws here.
    verifyBundle(bundleJson, keyServiceSpki);
    const hadSlot = ks.admit(id, bundleJson);
    assert.equal(hadSlot, true, "member has a slot in the base bundle");
    const plaintext = ks.open(KEYEXPR, envelope);
    assert.deepEqual(plaintext, message, "roundtrip plaintext matches");

    // Opening under a different topic must fail closed (the key-expr is AEAD-bound).
    assert.throws(() => ks.open(KEYEXPR + "/OTHER", envelope), DecryptError);

    // --- Rotate: destroy-by-default forward secrecy (re-wraps nothing). ---
    const receipt: RotationReceipt = id.rotate(freshScalar(), false, bundlesJson);
    assert.equal(receipt.policy, "destroy");
    assert.equal(receipt.now_unreadable_generations, 1);
    assert.equal(receipt.rewrapped_generations, 0);
    assert.match(receipt.timestamp_rfc3339, /Z$/);

    console.log(`opened: ${dec.decode(plaintext)}`);
    console.log(`seals remaining after one seal (cap 2): ${remaining}`);
    console.log("rotation receipt:", JSON.stringify(receipt));
    console.log("OK");
  } finally {
    // Explicit disposal is the only sanctioned release path (keys zeroize on close).
    ks.close();
    sk.close();
    id.close();
  }
}

main();
