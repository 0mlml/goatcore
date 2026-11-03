# Test keys — public, worthless, never bind one to a party

Ed25519 keys that sign the conformance vectors in this directory's parent. **The private seeds are
committed on purpose**, so that anyone can reproduce every signature in the vectors without
running our code.

Consequences, stated so nobody has to work them out:

- These keys are known to everyone who can read this repository.
- A package signed by one proves nothing about who produced it.
- Binding one to a `sovereign_id` (§5.1) would make that party impersonable by anyone.

They are for reproducing the vectors and for nothing else.

| File | What |
|---|---|
| `<sid>.seed.hex` | The 32-byte RFC 8032 private seed, hex |
| `<sid>.public.b64` | The 32-byte public key, base64 |
| `<sid>.public.hex` | The same public key, hex |

Rebuild either key from its seed:

```
python3 -c 'from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey; \
import sys,base64; s=bytes.fromhex(open(sys.argv[1]).read().strip()); \
print(base64.b64encode(Ed25519PrivateKey.from_private_bytes(s).public_key().public_bytes_raw()).decode())' xaa.seed.hex
```

The seeds are ASCII: `goat-federation-v1-test-key-xaa` and `...-xab`, each padded to 32 bytes with
one NUL.
