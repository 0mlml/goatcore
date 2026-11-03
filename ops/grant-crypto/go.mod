// grant-crypto — the data-access program M2 (grant-keyed encryption) crypto core.
//
// Phase E0/M2.0: the payload envelope + DEK wrapping primitives that machine-read
// enforcement is built on. Standard-library only (no third-party crypto), so the
// security-critical path stays auditable. See docs/design/grant-keyed-encryption.md.
module github.com/usareur-af/goatcore/ops/grant-crypto

go 1.24
