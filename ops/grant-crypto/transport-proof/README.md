# grant-crypto transport proof — E0/M2.0 on-fabric end-to-end

Completes **phase E0/M2.0** of grant-keyed encryption (grant-keyed-encryption.md §8): the `envelope` crypto core is carried over a **live Zenoh transport** and proven end to end. The 15 in-package unit tests prove the crypto in isolation; this proves it composes with the wire.

## Run

```bash
./run.sh
```

Self-contained: builds the `gce` CLI, ensures `eclipse-zenoh==1.9.0` (the fleet router pin, [zenoh-version-pinning.md](../../../docs/design/zenoh-version-pinning.md)) in a throwaway venv, and runs the harness against a two-peer loopback router link. Exit 0 = pass; a `FAIL:`/`DENIED` line names any failure. Only network is loopback Zenoh plus, on first run, the pip install.

## Why it is split Go + Python

When this proof was written there was **no maintained Go client for Zenoh 1.x** — the same gap [`rest-pub-bridge`](https://github.com/usareur-af/goatstuff/tree/main/ops/zenoh-shims/rest-pub-bridge) (ADR 1023) exists to close. (An official cgo `zenoh-go` has since shipped in Zenoh 1.9.0; it doesn't change what this proof demonstrates — the crypto is identical regardless of the transport's language — but it does reopen E1's transport choice, see grant-keyed-encryption.md §8 M2.1.) The proof runs the split E1 will run in production:

- **Crypto = Go** (`gce`, a thin CLI over the `envelope` package): the producer seals a payload and wraps the DEK to member SVID keys; a member unwraps its slot and opens the payload. E1 embeds the same package directly.
- **Transport = Python** (`harness.py`, native `eclipse-zenoh`): carries the artifacts over a real router and shells to `gce` for the crypto verdict. Stands in for the native cert-processes that consume bundles on the live fabric.

## What it proves

On a real router link (two peers, loopback TCP, multicast off):

1. The sealed payload and the `keys/<rs>/<gen>` bundle survive a Zenoh round-trip **byte-for-byte**.
2. Both generation-tag carriers work (§7 open decision 1): the **bundle** rides **in-band JSON** — the carrier a Go publisher via rest-pub-bridge *must* use, since the bridge PUT contract is body-only; the **data payload** rides a **Zenoh attachment** with the payload bytes untouched — the carrier a native producer can use. The consumer reassembles the envelope from `body + attachment` and it opens.
3. The authorized **member** unwraps its slot and recovers the plaintext (`gce open` exit 0).
4. An **outsider** with no slot in the bundle is **denied** (`gce open` exit 3) — the machine-plane read cut, enforced by the crypto, not the router. The router routed the same ciphertext to everyone; only the member could open it.

## Files

| File | Role |
|---|---|
| [`../cmd/gce/`](../cmd/gce/) | the Go crypto CLI (`gen-member` / `seal` / `open`); `main_test.go` is the in-process round-trip CI covers without Zenoh |
| `harness.py` | the live-Zenoh transport + orchestration + assertions |
| `run.sh` | build + venv + run driver |

## Not a production surface

`gce` is a proof/operator tool. E1 (the key-distribution service) imports the `envelope` package directly rather than shelling to the CLI. The self-signed P-256 `gen-member` leaves stand in for SPIRE-issued SVIDs (ADR 0412) — only the key type matters to the wrap.
