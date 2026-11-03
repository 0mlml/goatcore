/**
 * Conformance gate — the read-side merge gate (ADR 1106). Every case in the shared
 * `vectors.json` is driven through the binding's consumer path
 * (`verifyBundle → admit → open`) and its accept/reject decision (plus plaintext on
 * accept) must match the vector exactly. Expected rejects must also surface as the right
 * typed {@link GrantCryptoError} subclass — never a generic failure.
 *
 * One documented ABI nuance: the keystore indexes held DEKs by `(rs, gen)`, so the
 * `wrong generation` vector (whose advisory reason is `open-failed`) rejects as
 * {@link NoSlotError} — no DEK is held for the envelope's generation — rather than as a
 * {@link DecryptError}. Both are rejects, so accept/reject parity holds; the reason field
 * is advisory per the conformance spec. The `open-failed` reason therefore admits either
 * {@link DecryptError} (AEAD reject: wrong topic / tampered ct) or {@link NoSlotError}.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  Identity,
  KeyStore,
  verifyBundle,
  GrantCryptoError,
  BadSignatureError,
  NoSlotError,
  DecryptError,
  UnsupportedSuiteError,
  MalformedError,
} from "../src/index";
import {
  loadVectors,
  jsonBytes,
  hexToBytes,
  b64ToBytes,
  type CaseSpec,
  type VectorsDoc,
} from "./vectors";

const vectors = loadVectors();

// Advisory reason → the typed error subclass(es) a reject may legitimately surface as.
const REASON_CLASSES: Record<string, Array<new (...a: never[]) => GrantCryptoError>> = {
  "no-slot": [NoSlotError],
  "bad-signature": [BadSignatureError],
  "unsupported-suite": [UnsupportedSuiteError],
  "open-failed": [DecryptError, NoSlotError],
  malformed: [MalformedError],
};

test("vectors suite is the v1 suite this binding implements", () => {
  assert.equal(vectors.suite, "v1-ecdh-p256");
  assert.ok(vectors.cases.length > 0, "vectors must contain cases");
});

/**
 * Run one vector through verify → admit → open. Returns the plaintext on accept, or
 * throws the typed reject error. Mirrors the reference consumer order.
 */
function runCase(v: VectorsDoc, c: CaseSpec): Uint8Array {
  const art = v.artifacts[c.artifact];
  assert.ok(art, `unknown artifact ${c.artifact}`);
  const bundleBytes = jsonBytes(art.bundle);
  const envelopeBytes = jsonBytes(art.envelope);
  const keyexpr = c.keyexpr && c.keyexpr.length > 0 ? c.keyexpr : art.keyexpr;

  // 1. verify the bundle signature, if the case supplies a trust identity.
  if (c.verify && c.verify.length > 0) {
    const vk = v.keys[c.verify]?.pub_spki_b64;
    assert.ok(vk, `verify key ${c.verify} has no SPKI`);
    verifyBundle(bundleBytes, b64ToBytes(vk));
  }

  // 2/3. admit: unwrap the member's slot (had_slot === false is a normal outcome).
  const scalarHex = v.keys[c.member]?.priv_scalar_hex;
  assert.ok(scalarHex, `member ${c.member} has no private scalar`);
  const id = Identity.create(c.member, hexToBytes(scalarHex));
  const ks = KeyStore.create();
  try {
    ks.admit(id, bundleBytes);
    // 4. open with the case key-expression.
    return ks.open(keyexpr, envelopeBytes);
  } finally {
    ks.close();
    id.close();
  }
}

for (const c of vectors.cases) {
  test(`vector: ${c.name}`, () => {
    if (c.expect.result === "accept") {
      const pt = runCase(vectors, c);
      assert.ok(c.expect.plaintext_b64, "accept case must state plaintext_b64");
      assert.deepEqual(pt, b64ToBytes(c.expect.plaintext_b64), "plaintext mismatch");
      return;
    }

    // reject: must throw a typed GrantCryptoError of the reason-appropriate subclass.
    let thrown: unknown;
    try {
      runCase(vectors, c);
    } catch (e) {
      thrown = e;
    }
    assert.ok(thrown instanceof GrantCryptoError, `expected a GrantCryptoError, got ${thrown}`);
    const allowed = REASON_CLASSES[c.expect.reason ?? ""] ?? Object.values(REASON_CLASSES).flat();
    assert.ok(
      allowed.some((cls) => thrown instanceof cls),
      `reject reason "${c.expect.reason}" surfaced as ${(thrown as Error).constructor.name}, ` +
        `expected one of [${allowed.map((c2) => c2.name).join(", ")}]`,
    );
  });
}
