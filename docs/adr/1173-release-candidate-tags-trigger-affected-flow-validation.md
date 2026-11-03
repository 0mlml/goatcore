# ADR 1173 — Release-candidate tags trigger the affected end-to-end flows, on the dev goatnet, by nominators only

**Status:** Proposed (2026-09-08, operator-directed) · **Design:** [release-candidate-validation.md](../design/release-candidate-validation.md) · **Amends:** [ADR 1120](1120-release-graduation-ladder.md) (the dev→preprod hop's validation gate) · **Uses:** [ADR 1115](1115-goatnet-e2e-acceptance-suite.md) (the live acceptance rows)

## Context

A change that renames a control on a form and leaves the page's script reading the old id passes every server-side test and still leaves the deployed form doing nothing. Without a step that runs the deployed page in a browser, and without a separation between "merged" and "released", the merge is the release.

Two things were missing. A test that exercises a whole flow the way a person does, and a point in the process where the expensive version of that test is deliberately run, on a goatnet built for it, before a ref becomes a preprod candidate. The release ladder ([ADR 1120](1120-release-graduation-ladder.md)) already models a dev rung and a dev→preprod hop with a `validation-evidence` gate; the rung is `planned` and the gate has no named validator.

## Decision

1. **A release candidate is a git tag** `<component>/v<major>.<minor>.<patch>-rc.<n>` on a commit that is on `main`. The component prefix is the one the image workflows already trigger on (`goat-id-console/v*`, `brief/v*`, …), so tagging builds the candidate image once, tagged `v<semver>-rc.<n>`; the digest that graduates is the digest that was validated ([ADR 1120](1120-release-graduation-ladder.md) "build once, promote the digest").

2. **The tag is the control point for expensive validation.** Pull requests run only the cheap suites (mocked backend, minutes, no goatnet). Nothing touches a goatnet until a release candidate exists. This is where cost and timing are controlled: a nominator decides when the live run happens by cutting the tag.

3. **A change runs the flows it affects, computed, not remembered.** `ops/validation/flow-map.yaml` maps repository path globs to flows; each flow names its cheap suite and its live acceptance rows (`ops/validation/acceptance-tests.yaml` ids). `ops/validation/affected-flows.py` intersects a diff with the map. The release-candidate workflow uses the fleet's current pin for the component as the diff base, so the answer is "what changed since what the fleet runs", not "what changed in the last PR".

4. **The live run happens on the dev goatnet (zagros-dev), from a mesh-connected controller, and its PASS is the validation evidence** the dev→preprod hop requires. The workflow prints the exact commands; the run itself is `run-acceptance.py --env zagros-dev --only <affected rows>` after the candidate is deployed to zagros-dev as a declared deviation. Probes run from the controller because that is the path people traverse ([ADR 1115](1115-goatnet-e2e-acceptance-suite.md) decision 4); GitHub runners cannot reach the mesh and are not given credentials that could.

5. **Nomination is a repository role: admin.** The people who may cut a release candidate are those holding the **admin** role on the repository, and nobody else. That is GitHub's own permission model, read with the token every workflow already has, so the check needs no secret, no organisation read and no team lookup. Everyone else, internal team members and external collaborators alike, holds write, which lets them open pull requests and not cut candidates. The server-side half is in place: repository ruleset `release-candidate-tags` restricts creation, update and deletion of `refs/tags/*/v*-rc.*` to the admin role, so a non-admin push is refused before any workflow runs. The workflow role check stays as the second layer: it records who cut the candidate and holds if the ruleset is ever loosened.

6. **Coverage gaps are stated, never implied.** A flow with no live row lists `live: []`; the tool prints "live rows: none yet" for it, and changed files that match no flow are printed for a person to judge. `affected-flows.py --lint` fails CI when a flow names a suite directory or a manifest row that does not exist.

## Consequences

- The dev rung of `ops/release/ladder.yaml` was activated on 2026-09-09 after the first candidate (`goat-id-console/v1.0.0-rc.2`) went through by hand. The hand walk is now `ops/scripts/rc.sh <component> --version X.Y.Z --ack <name>`: tag, wait for the tag's validation and image, dev deviation landed, dev deploy verified against the host ledger, the affected live rows, the candidate record with its evidence, preprod deviation landed and deployed, rows again, convergence recorded. It stops at the soak; after the window `rc.sh <component> --tag <candidate> --from prod --ack <name>` graduates the record, folds the candidate into the fleet value, deploys and ledger-verifies prod, and re-verifies every rung. Fastlane (ADR 1127) now deploys and verifies the same way instead of printing the commands.
- Every browser suite or acceptance row added from now on adds or extends a `flow-map.yaml` entry in the same PR; the lint makes a dangling reference a CI failure.
- A release candidate that fails its live rows is abandoned by deleting the tag and cutting `-rc.<n+1>` from the fix; records are never rewritten.
- The intake form's flow (`candidate-intake`) is the first fully covered flow: cheap suite on PRs, live row `intake-form-browser-submit` on the candidate. The review-and-mint flow has Go tests and no browser or live row yet; the map says so.

## Alternatives considered

- **Run the live suites on every merge to main.** Rejected: every console merge would mint invites and drive a goatnet, several times an hour on busy days, with no human deciding it should. The tag is the decision.
- **Run live suites from GitHub-hosted runners.** Rejected: the runners are off-mesh, and putting a goatnet credential in a GitHub secret to reach the public intake page alone would validate one flow and create a standing credential outside the mesh.
- **Restrict nomination by convention only.** Rejected by the operator: the guard has to hold when nobody is looking.

Glossary: GLOSSARY.md.
