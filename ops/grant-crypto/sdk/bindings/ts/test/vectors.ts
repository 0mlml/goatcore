/**
 * Shared test helpers: locate and load the committed cross-language conformance
 * vectors, and small codec/key utilities. The vectors are the SAME
 * `ops/grant-crypto/envelope/testdata/vectors/vectors.json` the Go reference and the
 * Rust core validate against — this binding must reach byte-identical accept/reject
 * decisions.
 */

import * as fs from "node:fs";
import * as path from "node:path";
import * as crypto from "node:crypto";

export interface KeyRef {
  priv_scalar_hex?: string;
  pub_spki_b64?: string;
}
export interface Artifact {
  envelope: unknown;
  bundle: unknown;
  keyexpr: string;
}
export interface ExpectSpec {
  result: "accept" | "reject";
  plaintext_b64?: string;
  reason?: string;
}
export interface CaseSpec {
  name: string;
  artifact: string;
  member: string;
  verify?: string;
  keyexpr?: string;
  expect: ExpectSpec;
}
export interface VectorsDoc {
  suite: string;
  note: string;
  keys: Record<string, KeyRef>;
  artifacts: Record<string, Artifact>;
  cases: CaseSpec[];
}

/** Walk up from `start` until `envelope/testdata/vectors/vectors.json` is found. */
export function findVectorsDir(start: string = __dirname): string {
  let dir = start;
  for (let i = 0; i < 12; i++) {
    const p = path.join(dir, "envelope", "testdata", "vectors", "vectors.json");
    if (fs.existsSync(p)) return path.dirname(p);
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  throw new Error(
    `could not locate the conformance vectors starting from ${start}. ` +
      `Expected <grant-crypto>/envelope/testdata/vectors/vectors.json.`,
  );
}

export function loadVectors(): VectorsDoc {
  const file = path.join(findVectorsDir(), "vectors.json");
  return JSON.parse(fs.readFileSync(file, "utf8")) as VectorsDoc;
}

export const enc = new TextEncoder();
export const dec = new TextDecoder();

/** Serialize a parsed JSON value to UTF-8 bytes for a `*_json` ABI argument. */
export function jsonBytes(value: unknown): Uint8Array {
  return enc.encode(JSON.stringify(value));
}

export function hexToBytes(hex: string): Uint8Array {
  return Uint8Array.from(Buffer.from(hex, "hex"));
}
export function b64ToBytes(b64: string): Uint8Array {
  return Uint8Array.from(Buffer.from(b64, "base64"));
}

/** Generate a fresh, valid raw 32-byte P-256 private scalar (for rotate tests). */
export function freshP256Scalar(): Uint8Array {
  const { privateKey } = crypto.generateKeyPairSync("ec", { namedCurve: "P-256" });
  const jwk = privateKey.export({ format: "jwk" }) as { d?: string };
  if (!jwk.d) throw new Error("no private scalar in generated JWK");
  const raw = Buffer.from(jwk.d, "base64url");
  // JWK P-256 'd' is the big-endian scalar; left-pad to 32 bytes if a leading zero was dropped.
  const out = new Uint8Array(32);
  out.set(raw, 32 - raw.length);
  return out;
}
