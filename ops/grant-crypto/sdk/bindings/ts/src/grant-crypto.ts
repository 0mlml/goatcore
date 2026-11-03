/**
 * The grant-crypto public surface — the frozen five-noun / six-verb API
 * (`docs/design/grant-crypto-sdk.md`), rendered idiomatically for TypeScript over the
 * C ABI in {@link ./ffi}. Nothing is added, removed, or reinterpreted.
 *
 * Load-bearing properties, carried straight through from the ABI:
 * - **Keys never enter JS memory.** `Identity`, `KeyStore`, and `StreamKey` are opaque
 *   handles; the private scalar and every DEK live and die inside the Rust core. Only
 *   wire bytes (bundle/envelope JSON) and plaintext cross the boundary.
 * - **Explicit disposal is the only sanctioned release.** Call `close()` when done; it
 *   zeroizes the underlying secret (best-effort). A {@link FinalizationRegistry} only
 *   *warns* about a leaked handle — it never disposes (per the ADR 1106 lifecycle
 *   contract: a GC finalizer may report a leak, never release).
 * - **Fail-closed handles.** Any use after `close()` throws {@link DisposedHandleError};
 *   double-close throws the same (harmless).
 */

import { gc, koffi } from "./ffi";
import { Code, DisposedHandleError, errorForCode, GrantCryptoError } from "./errors";

/** Throw the typed error for a non-zero ABI status; return normally on `GC_OK`. */
function check(code: number): void {
  if (code === Code.Ok) return;
  const msg = gc.error_message(code) ?? `status ${code}`;
  throw errorForCode(code, `${msg} (code ${code})`);
}

/**
 * Copy a `gc_buf` handle's bytes into a fresh `Uint8Array`, then dispose the buffer
 * immediately (zeroizing the C-side copy). The returned array is the caller's own,
 * independent of any C memory.
 */
function takeBuffer(handle: number): Uint8Array {
  try {
    const len = Number(gc.buf_len(handle));
    if (len === 0) return new Uint8Array(0);
    const ptr = gc.buf_ptr(handle);
    if (!ptr) throw new GrantCryptoError(Code.Internal, "gc_buf_ptr returned NULL for a live buffer");
    const decoded = koffi.decode(ptr, koffi.array("uint8_t", len, "Typed")) as Uint8Array;
    const out = new Uint8Array(len);
    out.set(decoded);
    return out;
  } finally {
    // Best-effort dispose; the buffer is single-use and already copied out.
    gc.buf_dispose(handle);
  }
}

/** Marshal a value into a Buffer for a `const uint8_t*` argument (no copy for Buffers). */
function asBytes(data: Uint8Array): Buffer {
  return Buffer.isBuffer(data) ? data : Buffer.from(data.buffer, data.byteOffset, data.byteLength);
}

/**
 * Reject a string argument containing a NUL. koffi passes these as NUL-terminated C
 * strings, so an interior NUL would silently truncate the argument — throw
 * {@link MalformedError} client-side instead (matching the Go/Python bindings).
 */
function checkNoNul(value: string, name: string): void {
  if (value.includes("\u0000")) {
    throw errorForCode(Code.Malformed, `${name} must not contain an embedded NUL`);
  }
}

/** A single generation re-wrapped to the new key under `KeepAccess` (from the receipt). */
export interface ReWrappedDek {
  readonly rs: string;
  readonly gen: number;
  /** The re-wrapped slot, exactly as it rides the wire (opaque to callers). */
  readonly wrapped: unknown;
}

/**
 * The structured, self-dating record of a rotation — counts and an absolute timestamp,
 * never time-relative prose (ADR 1106). `rewrapped` is present only under `KeepAccess`.
 */
export interface RotationReceipt {
  readonly member_id: string;
  readonly policy: "destroy" | "keep-access";
  /** Distinct pre-rotation generations that become un-re-fetchable once the old key is gone. */
  readonly now_unreadable_generations: number;
  /** Distinct generations re-wrapped to the new key (`0` for `Destroy`). */
  readonly rewrapped_generations: number;
  /** Absolute UTC instant of the rotation, RFC3339 (`YYYY-MM-DDTHH:MM:SSZ`). */
  readonly timestamp_rfc3339: string;
  /** Present only under `KeepAccess`: the DEKs re-wrapped to the new key, to persist. */
  readonly rewrapped?: ReWrappedDek[];
}

const EMPTY_BUNDLES = Buffer.from("[]");

/**
 * A member identity — its id plus private key material, held opaquely in the core
 * (the scalar never enters JS memory). Unwrap-capable. Construct from the member's
 * enrollment credential (a raw 32-byte P-256 scalar).
 */
export class Identity {
  #handle: number;
  #closed = false;
  static #registry = leakRegistry("Identity");

  private constructor(handle: number) {
    this.#handle = handle;
    Identity.#registry.register(this, "Identity", this);
  }

  /**
   * Construct a member identity from its id and a raw 32-byte P-256 scalar. Throws
   * {@link MalformedError} if the scalar is not a valid key (validated now, so a bad
   * credential fails at construction, not at admit).
   */
  static create(memberId: string, scalar32: Uint8Array): Identity {
    if (scalar32.length !== 32) {
      throw errorForCode(Code.Malformed, "scalar32 must be exactly 32 bytes");
    }
    checkNoNul(memberId, "memberId");
    const out: [number] = [0];
    check(gc.identity_new(memberId, asBytes(scalar32), out));
    return new Identity(out[0]);
  }

  /** @internal — the raw ABI handle, for sibling verbs in this module only. */
  get handle(): number {
    if (this.#closed) throw new DisposedHandleError(Code.DisposedHandle, "identity is closed");
    return this.#handle;
  }

  /**
   * Rotate this identity's leaf key to `newScalar32` (raw 32-byte P-256 scalar). The
   * handle swaps to the new scalar in place (the old one is zeroized).
   *
   * `keepAccess = false` (the default, `Destroy`) drops the old key and re-wraps
   * nothing — full forward secrecy. `keepAccess = true` re-wraps this member's still-live
   * DEKs from `bundlesJson` (a JSON array of bundles) to the new key, locally, **reducing**
   * forward secrecy for those generations; the returned receipt then carries `rewrapped`.
   */
  rotate(newScalar32: Uint8Array, keepAccess = false, bundlesJson?: Uint8Array): RotationReceipt {
    if (newScalar32.length !== 32) {
      throw errorForCode(Code.Malformed, "newScalar32 must be exactly 32 bytes");
    }
    const bundles = bundlesJson ? asBytes(bundlesJson) : EMPTY_BUNDLES;
    const out: [number] = [0];
    check(
      gc.rotate(this.handle, asBytes(newScalar32), keepAccess, bundles, bundles.length, out),
    );
    const json = new TextDecoder().decode(takeBuffer(out[0]));
    return JSON.parse(json) as RotationReceipt;
  }

  /** Dispose the identity (zeroizes the scalar). Double-close throws {@link DisposedHandleError}. */
  close(): void {
    check(gc.identity_dispose(this.#handle));
    this.#closed = true;
    Identity.#registry.unregister(this);
  }

  [Symbol.dispose](): void {
    disposeQuietly(() => this.close());
  }
}

/**
 * The consumer key store: holds DEKs unwrapped from admitted bundles, keyed by
 * `(release-set, generation)`, and opens data messages with them. Nothing persists —
 * the store lives in the core and is wiped on `close()`.
 */
export class KeyStore {
  #handle: number;
  #closed = false;
  static #registry = leakRegistry("KeyStore");

  private constructor(handle: number) {
    this.#handle = handle;
    KeyStore.#registry.register(this, "KeyStore", this);
  }

  /** Create an empty key store. */
  static create(): KeyStore {
    const out: [number] = [0];
    check(gc.keystore_new(out));
    return new KeyStore(out[0]);
  }

  #liveHandle(): number {
    if (this.#closed) throw new DisposedHandleError(Code.DisposedHandle, "keystore is closed");
    return this.#handle;
  }

  /**
   * Admit a (previously verified) bundle for `id`: unwrap this member's slot and retain
   * the DEK for `(bundle.rs, bundle.gen)`. Returns whether the member had a slot — `false`
   * for an outsider is a normal outcome, not an error (nothing is retained). Throws only
   * on a malformed slot. **Verify the bundle first** with {@link verifyBundle}.
   */
  admit(id: Identity, bundleJson: Uint8Array): boolean {
    const bytes = asBytes(bundleJson);
    const hadSlot: [boolean] = [false];
    check(gc.keystore_admit(this.#liveHandle(), id.handle, bytes, bytes.length, hadSlot));
    return hadSlot[0];
  }

  /**
   * Open a data message received on `keyexpr` (its in-band JSON body) with the held DEK
   * for the message's `(rs, gen)`. Throws {@link NoSlotError} if no DEK is held for that
   * `(rs, gen)`, or a reject error ({@link DecryptError}, {@link UnsupportedSuiteError},
   * {@link MalformedError}) on failure. Returns a fresh `Uint8Array` of plaintext.
   */
  open(keyexpr: string, envelopeJson: Uint8Array): Uint8Array {
    checkNoNul(keyexpr, "keyexpr");
    const bytes = asBytes(envelopeJson);
    const out: [number] = [0];
    check(gc.keystore_open(this.#liveHandle(), keyexpr, bytes, bytes.length, out));
    return takeBuffer(out[0]);
  }

  /** Dispose the key store (zeroizes all held DEKs). Double-close throws {@link DisposedHandleError}. */
  close(): void {
    check(gc.keystore_dispose(this.#handle));
    this.#closed = true;
    KeyStore.#registry.unregister(this);
  }

  [Symbol.dispose](): void {
    disposeQuietly(() => this.close());
  }
}

/**
 * A producer's acquired stream key — a seal-capable key unwrapped from the producer's
 * own bundle slot (never from a recipient list; `seal` takes no audience). Enforces a
 * cumulative message-count cap below the GCM nonce-collision bound: at the cap, `seal`
 * refuses with {@link RotationRequiredError} until the identity rotates.
 */
export class StreamKey {
  #handle: number;
  #closed = false;
  static #registry = leakRegistry("StreamKey");

  private constructor(handle: number) {
    this.#handle = handle;
    StreamKey.#registry.register(this, "StreamKey", this);
  }

  /**
   * Acquire the producer's stream key from a JSON array of bundles: pick the highest
   * generation of `rs` in which `id` has a slot and unwrap it. Throws {@link NoSlotError}
   * if the producer isn't in any visible audience.
   */
  static acquire(bundlesJson: Uint8Array, rs: string, id: Identity): StreamKey {
    checkNoNul(rs, "rs");
    const bytes = asBytes(bundlesJson);
    const out: [number] = [0];
    check(gc.streamkey_acquire(bytes, bytes.length, rs, id.handle, out));
    return new StreamKey(out[0]);
  }

  #liveHandle(): number {
    if (this.#closed) throw new DisposedHandleError(Code.DisposedHandle, "stream key is closed");
    return this.#handle;
  }

  /**
   * Seal `plaintext` for publication on `keyexpr` under this stream key. `keyexpr` is
   * bound as AEAD additional data. Returns the serialized `Envelope` (the in-band wire
   * body) as a fresh `Uint8Array`. Throws {@link RotationRequiredError} once the cap is
   * reached.
   */
  seal(keyexpr: string, plaintext: Uint8Array): Uint8Array {
    checkNoNul(keyexpr, "keyexpr");
    const bytes = asBytes(plaintext);
    const out: [number] = [0];
    check(gc.streamkey_seal(this.#liveHandle(), keyexpr, bytes, bytes.length, out));
    return takeBuffer(out[0]);
  }

  /**
   * The number of messages that may still be sealed under this key before rotation is
   * required (the structured approach-to-cap fact). Returned as a `number` when it fits
   * in a safe integer (it always does at the default cap), otherwise as a `bigint`.
   */
  remaining(): number | bigint {
    const out: [number | bigint] = [0];
    check(gc.streamkey_remaining(this.#liveHandle(), out));
    const v = out[0];
    if (typeof v === "bigint") return v <= BigInt(Number.MAX_SAFE_INTEGER) ? Number(v) : v;
    return v;
  }

  /**
   * Tighten the seal cap (a caller may tighten, never loosen). A request above the
   * built-in default cap is refused with {@link MalformedError}.
   */
  setCap(n: number | bigint): void {
    check(gc.streamkey_set_cap(this.#liveHandle(), n));
  }

  /** Dispose the stream key (zeroizes the DEK). Double-close throws {@link DisposedHandleError}. */
  close(): void {
    check(gc.streamkey_dispose(this.#handle));
    this.#closed = true;
    StreamKey.#registry.unregister(this);
  }

  [Symbol.dispose](): void {
    disposeQuietly(() => this.close());
  }
}

/**
 * Verify a bundle's signature against the key-service verifying key (SPKI DER). Returns
 * normally on success; throws {@link BadSignatureError} or {@link MalformedError} on
 * failure. Consumers MUST verify before {@link KeyStore.admit}.
 */
export function verifyBundle(bundleJson: Uint8Array, vkSpkiDer: Uint8Array): void {
  const b = asBytes(bundleJson);
  const vk = asBytes(vkSpkiDer);
  check(gc.verify_bundle(b, b.length, vk, vk.length));
}

/** The ABI version the loaded core reports (monotone; core + binding ship in lockstep). */
export function abiVersion(): number {
  return gc.abi_version();
}

// ---- leak detection (never disposal) ----

/**
 * A {@link FinalizationRegistry} that warns — once, to stderr — when a handle of `kind`
 * is garbage-collected without an explicit `close()`. Per ADR 1106 the finalizer must
 * NEVER dispose (that is the caller's job); this only surfaces the leak so it gets fixed.
 * `unregister` is called from `close()`, so a properly closed handle warns nothing.
 */
function leakRegistry(kind: string): FinalizationRegistry<string> {
  return new FinalizationRegistry<string>(() => {
    process.emitWarning(
      `a grant-crypto ${kind} was garbage-collected without close() — its key material ` +
        `was not deterministically zeroized. Call close() (or use \`using\`) when done.`,
      { type: "ResourceLeakWarning" },
    );
  });
}

/** Run `fn`, swallowing a DisposedHandleError so `Symbol.dispose` is idempotent under `using`. */
function disposeQuietly(fn: () => void): void {
  try {
    fn();
  } catch (e) {
    if (!(e instanceof DisposedHandleError)) throw e;
  }
}
