# Zenoh ACL split probes

Two bounded `eclipse-zenoh` Python probes that establish the **before/after** evidence for the
ADR 1020 ingress/egress ACL split.

- **`acl-baseline-probe.py`** — authorized round-trip. Opens an mTLS session as the vendor,
  subscribes `<vendor>/**`, publishes `<vendor>/moon-pod-baseline/ping`, asserts it comes back.
  Exits 0 on PASS. This is the **delivery baseline the split must preserve** (the F-135 guard).
- **`acl-squat-probe.py`** — foreign-prefix squat. Same vendor cert publishes to a prefix it
  does NOT own. BEFORE the split: delivered (the over-grant). AFTER: should be denied / no delivery.

## Config (env vars, no committed secrets)

The vendor mTLS cert bundle is **operator-local and not committed**. Point the probes at it:

```
export NOVA_CERT_DIR=~/.goat/state/jailbird-shared    # holds <vendor>-cert.pem, <vendor>-key.pem, nova-ca-root.pem
export NOVA_VENDOR=jailbird                                   # cert basename + the prefix it owns
export NOVA_ROUTER=tls/zenoh.nova.netbird.nova.example:7447   # default; override if needed
```

Run (needs mesh access to the target goatnet + a venv with `eclipse-zenoh`):

```
python3 acl-baseline-probe.py     # expect RESULT PASS (both before and after the split)
python3 acl-squat-probe.py        # BEFORE: SQUAT SUCCEEDED ; AFTER split: no delivery
```

## AFTER-run note

For an unambiguous AFTER result, run the squat **subscriber** from a *second, different* vendor
cert/session (not the publishing cert) so a deny can't be masked by the publisher seeing its own
local sample.
