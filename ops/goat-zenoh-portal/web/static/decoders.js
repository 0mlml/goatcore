// Format decoders for the inspector. Trial-scope: tiny, fast, no
// external dependencies. The shapes producers actually use are the
// common shapes; we cover them. Edge-case wire formats (CBOR tagged
// values past tag 36, MessagePack ext, etc.) fall back to hexDump in
// the caller.
//
// Each decode function takes a Uint8Array and returns the decoded value,
// or throws on malformed input.

// --- CBOR (RFC 8949 subset) ------------------------------------------
// Supports: positive/negative ints (major 0/1), byte strings (major 2),
// text strings (major 3), arrays (major 4), maps (major 5), booleans
// (major 7 simple), null (major 7 22), undefined (major 7 23),
// half/single/double floats (major 7 25/26/27). Indefinite-length
// containers supported.

export function cborDecode(bytes) {
  const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const state = { dv, off: 0, bytes };
  const v = cborReadItem(state);
  if (state.off !== bytes.byteLength) {
    // Trailing data is acceptable but worth noting; the inspector
    // displays whatever returns.
  }
  return v;
}

function cborReadItem(s) {
  const b = s.dv.getUint8(s.off++);
  const major = b >> 5;
  const minor = b & 0x1f;
  const len = cborReadLen(s, minor);
  switch (major) {
    case 0: return Number(len);
    case 1: return -1 - Number(len);
    case 2: { // byte string
      if (len === null) { // indefinite
        const parts = [];
        while (s.dv.getUint8(s.off) !== 0xff) {
          parts.push(cborReadItem(s));
        }
        s.off++; // consume break
        return concatBytes(parts);
      }
      const out = s.bytes.slice(s.off, s.off + Number(len));
      s.off += Number(len);
      return out;
    }
    case 3: { // text string
      if (len === null) {
        const parts = [];
        while (s.dv.getUint8(s.off) !== 0xff) parts.push(cborReadItem(s));
        s.off++;
        return parts.join("");
      }
      const slice = s.bytes.slice(s.off, s.off + Number(len));
      s.off += Number(len);
      return new TextDecoder("utf-8").decode(slice);
    }
    case 4: { // array
      const out = [];
      if (len === null) {
        while (s.dv.getUint8(s.off) !== 0xff) out.push(cborReadItem(s));
        s.off++;
      } else {
        for (let i = 0; i < Number(len); i++) out.push(cborReadItem(s));
      }
      return out;
    }
    case 5: { // map
      const out = {};
      if (len === null) {
        while (s.dv.getUint8(s.off) !== 0xff) {
          const k = cborReadItem(s);
          out[String(k)] = cborReadItem(s);
        }
        s.off++;
      } else {
        for (let i = 0; i < Number(len); i++) {
          const k = cborReadItem(s);
          out[String(k)] = cborReadItem(s);
        }
      }
      return out;
    }
    case 6: { // tag — strip the tag, return inner
      return cborReadItem(s);
    }
    case 7: { // simple / float
      if (minor === 20) return false;
      if (minor === 21) return true;
      if (minor === 22) return null;
      if (minor === 23) return undefined;
      if (minor === 25) { const v = halfFloat(s.dv.getUint16(s.off - 2)); return v; }
      if (minor === 26) { const v = s.dv.getFloat32(s.off - 4); return v; }
      if (minor === 27) { const v = s.dv.getFloat64(s.off - 8); return v; }
      if (minor === 31) return Symbol.for("cbor-break");
      return Number(len);
    }
  }
  throw new Error("unsupported CBOR major type " + major);
}

function cborReadLen(s, minor) {
  if (minor < 24) return minor;
  if (minor === 24) return s.dv.getUint8(s.off++);
  if (minor === 25) { const v = s.dv.getUint16(s.off); s.off += 2; return v; }
  if (minor === 26) { const v = s.dv.getUint32(s.off); s.off += 4; return v; }
  if (minor === 27) { const v = s.dv.getBigUint64(s.off); s.off += 8; return v; }
  if (minor === 31) return null; // indefinite
  throw new Error("malformed CBOR length minor=" + minor);
}

function halfFloat(u) {
  // RFC 8949 appendix D
  const exp = (u & 0x7c00) >> 10;
  const mant = u & 0x03ff;
  let val;
  if (exp === 0) val = Math.pow(2, -14) * (mant / 1024);
  else if (exp !== 31) val = Math.pow(2, exp - 15) * (1 + mant / 1024);
  else val = mant === 0 ? Infinity : NaN;
  return (u & 0x8000) ? -val : val;
}

function concatBytes(parts) {
  let n = 0;
  for (const p of parts) n += p.length;
  const out = new Uint8Array(n);
  let o = 0;
  for (const p of parts) { out.set(p, o); o += p.length; }
  return out;
}

// --- MessagePack ------------------------------------------------------
// Hand-rolled subset covering: fixed-int, int8/16/32/64, uint8/16/32/64,
// fixstr / str8/16/32, fixmap / map16/32, fixarray / array16/32, nil,
// true, false, bin8/16/32, float32/64. Doesn't cover ext types — those
// fall through to bytes via the inspector hex fallback.

export function msgpackDecode(bytes) {
  const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  return mpReadItem({ dv, off: 0, bytes });
}

function mpReadItem(s) {
  const b = s.dv.getUint8(s.off++);
  if (b < 0x80) return b; // positive fixint
  if (b >= 0xe0) return b - 0x100; // negative fixint
  if (b >= 0x80 && b <= 0x8f) return mpReadMap(s, b & 0x0f);
  if (b >= 0x90 && b <= 0x9f) return mpReadArray(s, b & 0x0f);
  if (b >= 0xa0 && b <= 0xbf) return mpReadStr(s, b & 0x1f);
  switch (b) {
    case 0xc0: return null;
    case 0xc2: return false;
    case 0xc3: return true;
    case 0xc4: return mpReadBin(s, s.dv.getUint8(s.off++));
    case 0xc5: { const n = s.dv.getUint16(s.off); s.off += 2; return mpReadBin(s, n); }
    case 0xc6: { const n = s.dv.getUint32(s.off); s.off += 4; return mpReadBin(s, n); }
    case 0xca: { const v = s.dv.getFloat32(s.off); s.off += 4; return v; }
    case 0xcb: { const v = s.dv.getFloat64(s.off); s.off += 8; return v; }
    case 0xcc: return s.dv.getUint8(s.off++);
    case 0xcd: { const v = s.dv.getUint16(s.off); s.off += 2; return v; }
    case 0xce: { const v = s.dv.getUint32(s.off); s.off += 4; return v; }
    case 0xcf: { const v = s.dv.getBigUint64(s.off); s.off += 8; return v; }
    case 0xd0: { const v = s.dv.getInt8(s.off++); return v; }
    case 0xd1: { const v = s.dv.getInt16(s.off); s.off += 2; return v; }
    case 0xd2: { const v = s.dv.getInt32(s.off); s.off += 4; return v; }
    case 0xd3: { const v = s.dv.getBigInt64(s.off); s.off += 8; return v; }
    case 0xd9: return mpReadStr(s, s.dv.getUint8(s.off++));
    case 0xda: { const n = s.dv.getUint16(s.off); s.off += 2; return mpReadStr(s, n); }
    case 0xdb: { const n = s.dv.getUint32(s.off); s.off += 4; return mpReadStr(s, n); }
    case 0xdc: { const n = s.dv.getUint16(s.off); s.off += 2; return mpReadArray(s, n); }
    case 0xdd: { const n = s.dv.getUint32(s.off); s.off += 4; return mpReadArray(s, n); }
    case 0xde: { const n = s.dv.getUint16(s.off); s.off += 2; return mpReadMap(s, n); }
    case 0xdf: { const n = s.dv.getUint32(s.off); s.off += 4; return mpReadMap(s, n); }
  }
  throw new Error("msgpack: unsupported tag 0x" + b.toString(16));
}

function mpReadStr(s, n) {
  const out = new TextDecoder("utf-8").decode(s.bytes.slice(s.off, s.off + n));
  s.off += n;
  return out;
}
function mpReadBin(s, n) {
  const out = s.bytes.slice(s.off, s.off + n);
  s.off += n;
  return out;
}
function mpReadArray(s, n) {
  const out = [];
  for (let i = 0; i < n; i++) out.push(mpReadItem(s));
  return out;
}
function mpReadMap(s, n) {
  const out = {};
  for (let i = 0; i < n; i++) {
    const k = mpReadItem(s);
    out[String(k)] = mpReadItem(s);
  }
  return out;
}
