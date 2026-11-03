// grantcrypto — the idiomatic Go binding over the grant-crypto C ABI (ADR 1106,
// Phase 2). A thin, hand-written wrapper: opaque handles stay inside the core,
// key material never enters Go memory, wire structures (bundle/envelope/receipt
// JSON) cross as bytes. See docs/design/grant-crypto-sdk.md for the frozen API.
//
// Own module (no dependency on the root grant-crypto module) so the SDK subtree
// stays self-contained per the design's "directory move to a sibling repo"
// property. Links the core statically via cgo; see doc.go for build notes.
module github.com/usareur-af/goatcore/ops/grant-crypto/sdk/bindings/go

go 1.22
