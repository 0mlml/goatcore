// SPDX-License-Identifier: Apache-2.0
//
// What the release-readiness view says when the document is missing, older
// than the tree, or full of unknowns.
//
// The view computes no ladder fact, so these tests are not about grading —
// they are about the three ways a readiness page lies: rendering an absent
// document as an empty (therefore calm) table, flattening "not asked" into
// "no", and showing a soak timer for a clock that never started. Each has an
// assertion below.

use super::*;

const NOW_MS: i64 = 1_786_530_000_000; // 2026-08-12T05:00:00Z

fn doc_json(components: &str) -> String {
    format!(
        r#"{{"schema":"release-readiness-v1","generated_unix_ms":1786443600000,
 "generated_date":"2026-08-11",
 "source":{{"head_commit":"e9a2d80b","git":"available"}},
 "ladder":{{"rungs":[{{"name":"preprod","goatnet":"zagros"}},{{"name":"prod","goatnet":"zagros_prod"}}],
   "hop":{{"from":"preprod","to":"prod","gates":[{{"id":"soak","min_days":1}},{{"id":"operator-ack"}}]}}}},
 "unknowable":["Images built on main but pinned nowhere are not listed."],
 "components":[{components}]}}"#
    )
}

/// An aligned component with nothing open — the common case. Both rungs run a
/// build nothing has invalidated: the per-component reading counts 0.
const ALIGNED: &str = r#"{"component":"portal","class":"ladder","grade":"aligned",
 "var":"portal_image","fleet":"ghcr.io/{{ ghcr_org }}/goat-zenoh-portal:sha-f894c52@sha256:cbf7afeb",
 "image_workflow":{"workflow":".github/workflows/goat-zenoh-portal-image.yml",
   "paths":["ops/goat-zenoh-portal/**"],"why":null},
 "rungs":[
  {"rung":"preprod","goatnet":"zagros","state":"value",
   "value":"ghcr.io/{{ ghcr_org }}/goat-zenoh-portal:sha-f894c52@sha256:cbf7afeb",
   "deviates_from_fleet":false,
   "build":{"tag":"sha-f894c52","known":true,"commit_date":"2026-08-11T10:00:00Z","commits_on_head_since":12,
    "component_commits":{"known":true,"count":0,"newest":null,
     "workflow":".github/workflows/goat-zenoh-portal-image.yml","paths":["ops/goat-zenoh-portal/**"]}}},
  {"rung":"prod","goatnet":"zagros_prod","state":"value",
   "value":"ghcr.io/{{ ghcr_org }}/goat-zenoh-portal:sha-f894c52@sha256:cbf7afeb",
   "deviates_from_fleet":false,"build":{"tag":"sha-f894c52","known":true,
    "component_commits":{"known":true,"count":0,"newest":null,
     "workflow":".github/workflows/goat-zenoh-portal-image.yml","paths":["ops/goat-zenoh-portal/**"]}}}],
 "member_deviations":[],"candidate":null,"readiness_state":"no-candidate","last_graduated":null,
 "ledger":{"status":"rows","path":"ops/ansible/roles/goat-zenoh-portal/deploy-history.yml",
  "newest":{"sha":"sha-f894c52","image":"ghcr.io/usareur-af/goat-zenoh-portal:sha-f894c52@sha256:cbf7afeb",
   "digest":"sha256:cbf7afeb","pr":2447,"merged_at":"2026-08-11T10:36:19Z","ordered_by":"merged_at"},
  "pin_deployed":{"fleet":true,"preprod":true,"prod":true},"newest_is_pinned":true}}"#;

/// A blocked candidate whose soak clock could not be read, whose ledger
/// question could not be asked, and whose build commit is not in the tree.
const UNKNOWNS: &str = r#"{"component":"microscope","class":"ladder","grade":"UNDECLARED-SKEW",
 "var":"microscope_image","fleet":null,
 "rungs":[
  {"rung":"preprod","goatnet":"zagros","state":"value","value":"ghcr.io/x/microscope@sha256:aaaa",
   "deviates_from_fleet":true,"deviation_reason":"pinned ahead for the F-999 repair",
   "build":{"tag":null,"known":false,"why":"ref carries no sha-<short> tag (digest-only pin)"}},
  {"rung":"prod","goatnet":"zagros_prod","state":"absent","value":null,"deviates_from_fleet":false,
   "build":{"known":false,"why":"no ref pinned"}}],
 "member_deviations":[{"member":"zagros_b_hosts","reason":"standby runs no route surface","absent":true}],
 "candidate":{"path":"ops/release/records/microscope/20260812-aaaa.yaml","class":"ladder",
  "candidate_ref":"ghcr.io/x/microscope@sha256:aaaa","opened":null,"reason":"repair",
  "evidence":{"validation":true,"preprod_convergence":false},
  "soak":{"state":"unknown","required_days":1,"why":"record `opened` is not a date: None"},
  "readiness":{"state":"blocked","ack_required":true,
   "blockers":["evidence.preprod_convergence is empty (name the goat health consistency band / console render / M-doc)",
               "operator-ack gate: the record names no operator_ack"],
   "operator_ack":null}},
 "readiness_state":"blocked","last_graduated":null,
 "ledger":{"status":"no-ledger-coverage","path":null,"newest":null,
  "pin_deployed":{"fleet":null,"preprod":null,"prod":null},"newest_is_pinned":null}}"#;

/// The case this page exists for: every rung pins a build that later commits
/// to the component's own build inputs have overtaken, and the open candidate's
/// soak was waived by a named operator. Its last graduation was waived too.
const BEHIND: &str = r#"{"component":"id-admin-console","class":"ladder","grade":"candidate",
 "var":"id_admin_console_image","fleet":"ghcr.io/{{ ghcr_org }}/goat-id-console:sha-aaaaaaa@sha256:1111",
 "image_workflow":{"workflow":".github/workflows/goat-id-console-image.yml",
   "paths":["ops/goat-id-console/**"],"why":null},
 "rungs":[
  {"rung":"preprod","goatnet":"zagros","state":"value",
   "value":"ghcr.io/{{ ghcr_org }}/goat-id-console:sha-bbbbbbb@sha256:2222","deviates_from_fleet":true,
   "deviation_reason":"RC (2026-08-12): the credential copy fix — record 20260812-2222.yaml",
   "build":{"tag":"sha-bbbbbbb","known":true,"commit_date":"2026-08-12T08:00:00Z","commits_on_head_since":9,
    "component_commits":{"known":true,"count":3,
     "newest":{"sha":"deadbee","date":"2026-08-12T15:00:00Z","subject":"fix(console): stop 401ing the admin tier"},
     "workflow":".github/workflows/goat-id-console-image.yml","paths":["ops/goat-id-console/**"]}}},
  {"rung":"prod","goatnet":"zagros_prod","state":"value",
   "value":"ghcr.io/{{ ghcr_org }}/goat-id-console:sha-aaaaaaa@sha256:1111","deviates_from_fleet":false,
   "build":{"tag":"sha-aaaaaaa","known":true,"commit_date":"2026-08-10T08:00:00Z","commits_on_head_since":40,
    "component_commits":{"known":true,"count":11,
     "newest":{"sha":"deadbee","date":"2026-08-12T15:00:00Z","subject":"fix(console): stop 401ing the admin tier"},
     "workflow":".github/workflows/goat-id-console-image.yml","paths":["ops/goat-id-console/**"]}}}],
 "member_deviations":[],
 "candidate":{"path":"ops/release/records/id-admin-console/20260812-2222.yaml","class":"ladder",
  "candidate_ref":"ghcr.io/{{ ghcr_org }}/goat-id-console:sha-bbbbbbb@sha256:2222",
  "opened":"2026-08-12","reason":"credential copy follows the goatnet's flow",
  "evidence":{"validation":true,"preprod_convergence":true},
  "soak":{"state":"waived","required_days":1,"opened":"2026-08-12","elapsed_days":0,
   "remaining_days":1,"clock":"record `opened` date",
   "waiver":{"by":"person-900@example.org","attribution":"named",
    "reason":"prod tells developers the opposite of their own welcome email"}},
  "readiness":{"state":"soak-waived","ack_required":true,"operator_ack":"person-900@example.org",
   "blockers":["soak: opened 2026-08-12, needs >= 1 day(s)"],
   "soak_waiver":{"by":"person-900@example.org","attribution":"named",
    "reason":"prod tells developers the opposite of their own welcome email"}}},
 "readiness_state":"soak-waived",
 "last_graduated":{"ref":"ghcr.io/{{ ghcr_org }}/goat-id-console:sha-aaaaaaa@sha256:1111",
  "on":"2026-08-11","ack":"person-900@example.org","path":"ops/release/records/id-admin-console/20260811-1111.yaml",
  "soak_waiver":{"by":null,"attribution":"unknown","reason":"operator directive, author not recorded"}},
 "ledger":{"status":"rows","path":"ops/ansible/roles/id-admin-console/deploy-history.yml",
  "newest":{"sha":"sha-aaaaaaa","image":"ghcr.io/usareur-af/goat-id-console:sha-aaaaaaa@sha256:1111",
   "digest":"sha256:1111","pr":2461,"merged_at":"2026-08-11T10:36:19Z","ordered_by":"merged_at"},
  "pin_deployed":{"fleet":true,"preprod":false,"prod":true},"newest_is_pinned":true}}"#;

fn parse(components: &str) -> ReleaseDoc {
    serde_json::from_str(&doc_json(components)).expect("fixture parses")
}

#[test]
fn absent_document_refuses_rather_than_rendering_an_empty_table() {
    let err = load("/nonexistent/release-readiness.json").unwrap_err();
    let html = render_page(&Err(err.clone()), NOW_MS, "0.1.0");
    assert!(html.contains("REFUSED"), "an absent document must refuse: {html}");
    assert!(
        html.contains("/nonexistent/release-readiness.json"),
        "the refusal must name the path it read"
    );
    assert!(
        html.contains("graduate.py readiness"),
        "the refusal must name what produces the document"
    );
    assert!(!html.contains("<tbody>"), "a refusal renders no table at all");
}

#[test]
fn a_document_of_another_schema_is_refused_not_guessed_at() {
    let dir = std::env::temp_dir().join(format!("rel-schema-{}", std::process::id()));
    std::fs::create_dir_all(&dir).unwrap();
    let path = dir.join("doc.json");
    std::fs::write(&path, r#"{"schema":"release-readiness-v9","components":[]}"#).unwrap();
    let err = load(path.to_str().unwrap()).unwrap_err();
    assert!(err.contains("release-readiness-v9") && err.contains("refusing"), "{err}");
    std::fs::remove_dir_all(&dir).ok();
}

#[test]
fn malformed_json_says_so_and_does_not_panic() {
    let dir = std::env::temp_dir().join(format!("rel-bad-{}", std::process::id()));
    std::fs::create_dir_all(&dir).unwrap();
    let path = dir.join("doc.json");
    std::fs::write(&path, "{not json").unwrap();
    let err = load(path.to_str().unwrap()).unwrap_err();
    assert!(err.contains("did not parse"), "{err}");
    std::fs::remove_dir_all(&dir).ok();
}

#[test]
fn the_document_age_and_its_source_commit_are_always_on_the_page() {
    let html = render_body(&parse(ALIGNED), NOW_MS);
    assert!(html.contains("2026-08-11"), "the generation date renders");
    assert!(html.contains("1d ago"), "the age renders: {html}");
    assert!(html.contains("e9a2d80b"), "the source commit renders");
    assert!(
        html.contains("SNAPSHOT"),
        "the page must say the document does not refresh itself"
    );
}

#[test]
fn a_document_with_no_generation_stamp_says_the_age_is_unstatable() {
    let mut doc = parse(ALIGNED);
    doc.generated_unix_ms = 0;
    let html = render_body(&doc, NOW_MS);
    assert!(html.contains("age unstatable"), "{html}");
    assert!(!html.contains("0m ago"), "a missing stamp must not render as freshly generated");
}

#[test]
fn not_asked_never_renders_as_no() {
    let html = render_body(&parse(UNKNOWNS), NOW_MS);
    assert!(html.contains("not asked"), "an unaskable ledger question says so: {html}");
    assert!(
        !html.contains("no deploy recorded"),
        "a null pin_deployed must not render as the amber 'no deploy recorded' finding"
    );
    // And the inverse: a real false DOES render as the finding.
    let html_ok = render_body(&parse(ALIGNED), NOW_MS);
    assert!(html_ok.contains("deployed"), "a true pin_deployed renders as deployed");
}

#[test]
fn an_unusable_soak_clock_shows_no_timer() {
    let html = render_body(&parse(UNKNOWNS), NOW_MS);
    assert!(html.contains("soak unknown"), "{html}");
    assert!(
        html.contains("not a date"),
        "the reason the clock could not start rides along"
    );
    assert!(
        !html.contains("0d elapsed") && !html.contains("remaining"),
        "no countdown for a clock that never started"
    );
}

#[test]
fn a_blocked_component_names_every_blocker_verbatim() {
    let html = render_body(&parse(UNKNOWNS), NOW_MS);
    assert!(html.contains("blocked"), "the state renders");
    assert!(
        html.contains("evidence.preprod_convergence is empty"),
        "the blocker text is the tool's own sentence: {html}"
    );
    assert!(html.contains("operator-ack gate"), "every blocker renders, not just the first");
}

#[test]
fn a_declared_deviation_renders_as_a_deviation_with_its_reason() {
    let html = render_body(&parse(UNKNOWNS), NOW_MS);
    assert!(html.contains("deviation"), "{html}");
    assert!(html.contains("pinned ahead for the F-999 repair"), "the stated reason renders");
    // Member-level deviations are a different axis and must not be dropped.
    assert!(html.contains("zagros_b_hosts") && html.contains("standby runs no route surface"));
}

#[test]
fn an_unknown_build_commit_is_stated_not_zeroed() {
    let html = render_body(&parse(UNKNOWNS), NOW_MS);
    assert!(html.contains("build commit unknown"), "{html}");
    assert!(html.contains("digest-only pin"), "the reason renders");
    let aligned = render_body(&parse(ALIGNED), NOW_MS);
    assert!(
        aligned.contains("12 later commit(s)") && aligned.contains("not this component's"),
        "the commits-since number must carry what it does and does not count: {aligned}"
    );
}

#[test]
fn undeclared_skew_is_the_only_red_and_aligned_is_green() {
    let skew = render_body(&parse(UNKNOWNS), NOW_MS);
    assert!(skew.contains("chip-red") && skew.contains("undeclared skew"), "{skew}");
    let aligned = render_body(&parse(ALIGNED), NOW_MS);
    assert!(aligned.contains("chip-green") && aligned.contains("aligned"));
    assert!(!aligned.contains("chip-red"), "an aligned component shows no red");
}

#[test]
fn a_grade_this_console_does_not_know_renders_as_itself() {
    let mut doc = parse(ALIGNED);
    doc.components[0].grade = "future-grade".into();
    doc.components[0].readiness_state = "future-state".into();
    let html = render_body(&doc, NOW_MS);
    assert!(html.contains("future-grade") && html.contains("future-state"), "{html}");
    for term in ["future-grade", "future-state"] {
        assert!(
            html.contains(&format!(r#"chip-idle" title="ladder grade: {term}"#))
                || html.contains(&format!(r#"chip-idle" title="{term} —"#)),
            "an unknown vocabulary term must render on the neutral chip, not a healthy one: {html}"
        );
    }
}

#[test]
fn every_component_occupies_a_row_and_the_rung_columns_come_from_the_ladder() {
    let html = render_body(&parse(&format!("{ALIGNED},{UNKNOWNS}")), NOW_MS);
    assert!(html.contains("portal") && html.contains("microscope"));
    assert!(html.contains("zagros_prod"), "the rung columns name their goatnets");
    assert!(html.contains("hop preprod &rarr; prod gates: soak (1d), operator-ack"), "{html}");
    // An absent value on a rung is a rendered answer, not a blank cell.
    assert!(html.contains("absent"));
}

#[test]
fn what_the_page_cannot_know_is_carried_from_the_document() {
    let html = render_body(&parse(ALIGNED), NOW_MS);
    assert!(html.contains("what this page cannot know"));
    assert!(html.contains("Images built on main but pinned nowhere"));
}

#[test]
fn document_values_are_escaped_not_injected() {
    let mut doc = parse(ALIGNED);
    doc.components[0].component = "<script>x</script>".into();
    let html = render_body(&doc, NOW_MS);
    assert!(!html.contains("<script>x</script>"));
    assert!(html.contains("&lt;script&gt;"));
}

#[test]
fn the_committed_generator_and_this_view_agree_on_the_schema_name() {
    // The one string that binds the Python producer to the Rust consumer. If
    // graduate.py's READINESS_SCHEMA changes, this fails here rather than as a
    // refusal on a deployed console.
    let py = std::fs::read_to_string(
        std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
            .ancestors()
            .nth(4)
            .unwrap()
            .join("ops/release/graduate.py"),
    )
    .expect("graduate.py is in the same tree");
    assert!(
        py.contains(&format!(r#"READINESS_SCHEMA = "{SCHEMA}""#)),
        "graduate.py must declare schema {SCHEMA}"
    );
}

#[test]
fn a_repeated_build_note_is_said_once_but_every_rung_keeps_its_value() {
    let html = render_body(&parse(ALIGNED), NOW_MS);
    assert_eq!(
        html.matches("built from sha-f894c52").count(),
        1,
        "the identical build note renders once, not per column: {html}"
    );
    assert!(html.contains("same as the rung to its left"));
    assert!(
        html.matches(r#">sha-f894c52<"#).count() >= 2,
        "both rungs still show which ref they hold"
    );
}

// --- the per-component "is there a build nothing pins?" reading -----------

#[test]
fn commits_to_a_components_build_inputs_read_as_a_newer_unpinned_image() {
    let html = render_body(&parse(BEHIND), NOW_MS);
    assert!(
        html.contains("newer build unpinned") && html.contains("almost certainly exists"),
        "the finding must be stated, not left to arithmetic: {html}"
    );
    assert!(
        html.contains("3 commit(s)") && html.contains("11 commit(s)"),
        "each rung reports its own distance, not one shared number"
    );
    assert!(
        html.contains("deadbee") && html.contains("stop 401ing the admin tier"),
        "the newest such commit is named so it can be looked up"
    );
    assert!(
        html.contains("goat-id-console-image.yml"),
        "the workflow whose paths filter grounds the count is named"
    );
    // Per rung the claim is rung-scoped — the preprod rung of a candidate DOES
    // pin a build from these commits, so only the component-level line may say
    // no pin names them.
    assert!(
        html.contains("this rung does not run it"),
        "a per-rung cell must not claim what the table as a whole pins: {html}"
    );
    // And the component-level claim: no rung pins a build as new as the source.
    assert!(
        html.contains("built, pinned nowhere") && html.contains("at least 3 commit(s)"),
        "the least-behind rung sets the component-level distance: {html}"
    );
}

#[test]
fn a_component_whose_sources_have_not_moved_reads_current_not_unknown() {
    let html = render_body(&parse(ALIGNED), NOW_MS);
    assert!(html.contains("build current"), "0 commits is a measurement: {html}");
    assert!(
        html.contains("a rung is current"),
        "the component-level line says a rung pins a current build"
    );
    assert!(
        !html.contains("newer build unpinned") && !html.contains("built, pinned nowhere"),
        "a current component must carry no unpinned-build finding"
    );
}

#[test]
fn an_unreadable_build_input_filter_is_unknown_and_never_zero() {
    let mut doc = parse(BEHIND);
    for r in doc.components[0].rungs.iter_mut() {
        r.build.component_commits = Some(ComponentCommits {
            known: false,
            why: Some(".github/workflows/x.yml declares no `on.push.paths` filter".into()),
            workflow: Some(".github/workflows/x.yml".into()),
            ..Default::default()
        });
    }
    let html = render_body(&doc, NOW_MS);
    assert!(html.contains("newer build unknown"), "{html}");
    assert!(html.contains("no `on.push.paths` filter"), "the reason rides along");
    assert!(
        !html.contains("build current") && !html.contains("0 commit"),
        "an unevaluable filter must never render as a current build"
    );
    assert!(
        html.contains("unpinned build unknown"),
        "the component-level claim degrades with its rungs"
    );
}

#[test]
fn a_document_from_an_older_generator_degrades_rather_than_inventing_a_reading() {
    // UNKNOWNS carries no component_commits at all — the shape a console
    // deployed after this change but reading an older document sees.
    let html = render_body(&parse(UNKNOWNS), NOW_MS);
    assert!(html.contains("newer build unknown") && html.contains("older generator"), "{html}");
    assert!(!html.contains("build current"));
}

#[test]
fn one_unknown_rung_withdraws_the_component_level_claim() {
    let mut doc = parse(BEHIND);
    doc.components[0].rungs[1].build.component_commits =
        Some(ComponentCommits { known: false, why: Some("no build commit".into()), ..Default::default() });
    let html = render_body(&doc, NOW_MS);
    assert!(
        html.contains("unpinned build unknown") && !html.contains("built, pinned nowhere"),
        "a claim about EVERY rung needs every rung readable: {html}"
    );
}

// --- document age must degrade, not merely print --------------------------

const DAY_MS: i64 = 86_400_000;

#[test]
fn a_fresh_document_is_marked_fresh_and_carries_no_degraded_state() {
    let html = render_body(&parse(ALIGNED), NOW_MS);
    assert!(html.contains(r#"chip-green" title="written by a console deploy"#), "{html}");
    assert!(!html.contains("prov-degraded"), "a fresh page shows no stale styling");
    assert!(!html.contains("Redeploy the consistency console"));
}

#[test]
fn a_document_older_than_a_week_says_stale_and_says_what_to_do() {
    let doc = parse(ALIGNED);
    let html = render_body(&doc, doc.generated_unix_ms + 9 * DAY_MS);
    assert!(html.contains("prov-degraded"), "the paragraph carries the degraded state: {html}");
    assert!(html.contains(">stale<"), "the chip names the state");
    assert!(html.contains("9d ago"), "the age still renders");
    assert!(
        html.contains("Redeploy the consistency console"),
        "a degraded state must say what to DO"
    );
    assert!(!html.contains("very stale"), "9 days is not the month-old state");
}

#[test]
fn a_month_old_document_degrades_further_and_says_to_trust_nothing() {
    let doc = parse(ALIGNED);
    let html = render_body(&doc, doc.generated_unix_ms + 45 * DAY_MS);
    assert!(html.contains("sev-red") && html.contains("very stale"), "{html}");
    assert!(html.contains("Read nothing on this page as current"));
    assert!(html.contains("Redeploy the consistency console"));
}

#[test]
fn a_stamp_from_the_future_reads_as_skew_not_as_freshness() {
    let doc = parse(ALIGNED);
    let html = render_body(&doc, doc.generated_unix_ms - DAY_MS);
    assert!(html.contains("clock skew") && html.contains("prov-degraded"), "{html}");
    assert!(html.contains("generated in the future"));
    assert!(!html.contains(r#">fresh<"#), "skew must not render as fresh");
}

#[test]
fn a_missing_stamp_is_degraded_too_not_merely_unlabelled() {
    let mut doc = parse(ALIGNED);
    doc.generated_unix_ms = 0;
    let html = render_body(&doc, NOW_MS);
    assert!(html.contains("age unstatable") && html.contains("prov-degraded"), "{html}");
    assert!(html.contains("Redeploy the consistency console"));
}

// --- soak waivers ---------------------------------------------------------

#[test]
fn a_waived_soak_reads_as_waived_with_its_author_and_reason() {
    let html = render_body(&parse(BEHIND), NOW_MS);
    assert!(html.contains("soak waived"), "{html}");
    assert!(html.contains("person-900@example.org"), "the waiver names WHO");
    assert!(
        html.contains("opposite of their own welcome email"),
        "the waiver names WHY"
    );
    assert!(
        html.contains("0d elapsed of a 1d window"),
        "the unmet window is still stated — a waiver is not a met soak"
    );
    assert!(
        html.contains("still refuses on the soak gate"),
        "the page must not imply the tool would bless this fold"
    );
    assert!(
        !html.contains(">soaking<"),
        "a waived soak must not render as a window nobody has looked at"
    );
}

#[test]
fn a_waiver_that_names_nobody_renders_as_unknown_attribution() {
    let mut doc = parse(BEHIND);
    if let Some(c) = doc.components[0].candidate.as_mut() {
        if let Some(w) = c.soak.waiver.as_mut() {
            w.by = None;
            w.attribution = "unknown".into();
        }
    }
    let html = render_body(&doc, NOW_MS);
    assert!(
        html.contains("soak waived — no attribution") && html.contains("attribution unknown"),
        "an unattributed waiver must look worse, not the same: {html}"
    );
    assert!(html.contains("chip-red"), "an unattributed waiver is not an amber-grade fact");
}

#[test]
fn a_graduation_that_waived_its_soak_says_so_where_it_is_recorded() {
    let html = render_body(&parse(BEHIND), NOW_MS);
    assert!(
        html.contains("last graduated") && html.contains("that graduation did not wait out"),
        "{html}"
    );
    assert!(
        html.contains("attribution unknown"),
        "the backfilled waiver's missing author renders as unknown, not as the ack"
    );
}

#[test]
fn soak_waived_is_a_state_of_its_own_and_is_not_ready() {
    let html = render_body(&parse(BEHIND), NOW_MS);
    assert!(html.contains(r#"chip-amber" title="soak-waived —"#), "{html}");
    assert!(
        !html.contains("ready to graduate"),
        "a waiver is not a pass — `graduate` still refuses"
    );
}

#[test]
fn the_page_renders_for_visual_review_like_the_drift_fixture() {
    // Same escape hatch the drift fixture uses: GCC_FIXTURE_DIR writes the
    // rendered page for a human to look at, and GCC_RELEASE_FIXTURE points at
    // a REAL document (`graduate.py readiness --out …`) so the review is
    // against the tree's own ladder rather than a toy one.
    let doc = match std::env::var("GCC_RELEASE_FIXTURE") {
        Ok(p) => load(&p).expect("the named fixture document loads"),
        Err(_) => parse(&format!("{ALIGNED},{UNKNOWNS}")),
    };
    let page = render_page(&Ok(doc), NOW_MS, "0.1.0");
    assert!(page.contains("release readiness") && page.contains("<table"));
    if let Ok(dir) = std::env::var("GCC_FIXTURE_DIR") {
        let dir = std::path::PathBuf::from(dir);
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(dir.join("release.html"), &page).unwrap();
        eprintln!("release page written to {}", dir.display());
    }
}
