# The golden corpus — the conformance contract for the grading grammar

The grammar in [`../grammar.yaml`](../grammar.yaml) has to execute in two
languages: Python repo-side (generators, CI gates, the stage-1 renderer — live
today) and Rust on-fabric (the CF-3 rollup meter must compute a colour to
publish an `EnvHealthV1` at all, and the CF-6 console renders live records —
both hold a long-running Zenoh mTLS session, which is Rust by ADR 0112's
decision test). Pretending one implementation could serve both would be false;
so would letting two implementations each define the grammar.

**The rule:** the grammar is defined once as data; an implementation is
permitted only if it passes this corpus in CI, and **no implementation may
define a grade**. A grade whose behaviour is not pinned by a case here is not
part of the grammar.

Canon: `docs/design/consistency-foundation.md`
§4.3 (ADR 1110 D2).

## Layout

| Path | What it pins |
|---|---|
| `cases/precedence.yaml` | Worst-wins: which grade a set of grades collapses to. The two ungrounded states outrank everything, so nothing reaches `match` on a value nobody could read. |
| `cases/rows.yaml` | One row at a time: cells in, grade + remediation + `why` + grounding out. |
| `cases/accountability.yaml` | `SKEW` vs `unaccountable`, computed from a reader's inputs only — one generation plus one observed content hash, no git tree. |
| `cases/renders.yaml` | The CF-4 render classification (`account_for_render`): a watched file's hash now vs what the last converge recorded. A hand-edit is `unaccountable`, never `SKEW` — a render has no generation to be behind. |
| `generations/*.intent.json` | A whole synthetic IntentV1 document, schema-valid against `../schema/intent-v1.schema.json`. |
| `generations/*.expected.json` | The exact graded output for that generation, row by row. |

Everything here is language-neutral data. Nothing in this directory imports
Python, so the Rust implementation reads the same bytes.

## Case format — `cases/rows.yaml`

```yaml
- name: skew-undeclared-repo          # unique; names the behaviour, not the fixture
  why_this_case: <one line>           # required — a case that cannot say what it
                                      # pins is a case nobody will maintain
  subject: component | capability | node_shape | retired | ungoverned
  row_name: <the component or var>
  goatnet: <name>                     # fills {env} in a remediation pointer
  vantage: repo | live
  declared: true | false              # does the authoring surface declare this difference
  artifact_type: image | config | acl | data | cert | forward-only   # optional
  cells:                              # column -> IntentV1 cell
    zagros_hosts:   { state: resolved, value: "true" }
    zagros_b_hosts: { state: resolved, value: "false" }
  expect:
    grade: SKEW
    why: "zagros_b_hosts=false, zagros_hosts=true — they differ and nothing declares why"
    grounding: { n: 2, vantage: repo }
    remediation:
      kind: declare
      refuses: false
      pointer: "…"                    # VERBATIM. If the pointer is wrong the bug
                                      # is here and in the grammar, where one fix
                                      # reaches every surface.
```

`expect.remediation: null` asserts that a grade needs no pointer — which is only
`match` and `declared`. Every other grade **must** carry one, and the library
refuses to emit a row that needs one and has none.

## Adding a case

A grade change, a precedence change or a remediation change lands as a **diff to
this directory** in the same commit as the code. That is the whole mechanism: a
reviewer reads what the surface will tell an operator to do, as a diff, instead
of inferring it from a renderer. If a grade edit ever lands without a corpus
diff, the one-grammar guarantee has already failed (ADR 1110's revisit trigger).

## What is deliberately not covered yet

`live`-vantage rows have their producers built (CF-3's meter and collector,
CF-4's render recorder) but **nothing is deployed** — no fleet node publishes a
`ConsistencyReportV1`, and the render recorder additionally ships default-off.
The grading, precedence and remediation dispatch of every live state are pinned
here anyway, so a deploy decision changes where the cells come from, never what
they mean.
