# ADR 1199 — One error catalog, compiled into goat and goatbuilder, interprets every error they print and every tool error they carry

| Field | Value |
|---|---|
| Status | **Accepted** (2026-10-28). On merge nothing changes on a live goatnet: both binaries gain an `explain` verb and an interpreted error path; the catalog and its gate land in the tree |
| Date | 2026-10-28 |
| Deciders | operator |
| Related | [`error-interpretation.md`](../design/error-interpretation.md) (the design) · [ADR 1019](1019-goat-cli-unified-operator-binary.md) (goat-cli, whose `E_*` codes and exit codes this keeps) · [ADR 1198](1198-goatbuilder-config-driven-deploy-release-cli.md) (goatbuilder, whose contract C10 names the machine-readable refusal) · [`goat-cli-error-codes.md`](../design/goat-cli-error-codes.md) |

## Decision

Every error `goat` or `goatbuilder` prints is interpreted before the operator sees it: what happened, the likely cause, what to do, where to read. The interpretation comes from one catalog, `ops/errors/catalog.yaml`, compiled into both binaries by a shared crate, `crates/goat-errors`. The catalog covers the binaries' own errors (goat's existing `E_*` namespace unchanged; `GB-NNN` for goatbuilder) and the errors of the tools they call (`X-<TOOL>-NNN`: ansible, ssh, op, aws-vault, git, gh, cosign, docker, netbird, http, dns, tls, python), each with regular expressions over the tool's own words.

Interpretation scans the **whole** text, because an error from either binary often carries another tool's output inside it. Three layers run in order and every hit is reported, innermost first: the codes the text carries; the catalog's patterns; a deterministic lexical nearest-neighbour (IDF-weighted word overlap, threshold 0.45) when the first two find nothing. A text nothing matches is said to be unrecognised, with one instruction: `explain --report -`, which writes the text and its interpretation to a file to share so the catalog grows.

Both binaries get an `explain` verb for a code or any text, and both route their top-level error path through the crate, so no bare message reaches an operator. A CI gate holds the catalog to its contract and keeps the generated reference, the goat codes doc and the citing code in step.

## Why

The operator's instruction, verbatim: "I want to avoid the situation that they output an error message that the user cannot immediately understand", and the reason a general store was considered: "error messages might include content in them that is not from the tool itself if spitting out the response from the call to something else". Deploy-path errors such as a stale checkout, a profile-name fallthrough or a malformed secret reference were understandable only to someone who had read the scripts.

A catalog with patterns answers both halves: the binaries' own refusals carry a code so they are understood wherever the text travels, and the tools' words are matched wherever they sit in the text.

## Alternatives

- **A vector database with an embedding model shipped in the binaries, as a semantic matcher.** Deferred, not rejected: it adds tens of megabytes to binaries that run on islanded sites, its answers change between model versions, and every error either binary meets today comes from its own code or from a fixed set of tools whose words are known. The crate's `Matcher` trait is the slot; a measured miss rate from `explain --report` files is what would bring it in.
- **A third, separate interpreter binary.** One more thing to install and keep in step; the interpretation belongs at the point the error is printed, so it is a crate both binaries link.
- **Keep `goat explain` reading the codes doc, add a goatbuilder equivalent.** Two homes for the same facts, no coverage of tool output; the codes doc stays as goat's namespace record and the gate ties it to the catalog.
- **Patterns only on the first line of an error.** Misses the passthrough case the operator named.

## Consequences

- An error with no catalog entry is a defect of the catalog, filed by `explain --report`; the catalog grows by YAML blocks.
- A new goatbuilder refusal cites a code or the gate fails; a new goat `E_*` variant needs its catalog entry and its codes-doc row.
- The lexical layer's threshold and the innermost-first ordering are the crate's tests; changing either changes those tests first.
