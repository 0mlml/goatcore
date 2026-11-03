// SPDX-License-Identifier: Apache-2.0
//
// render.rs — server-side HTML for the consistency console.
//
// Everything here is PRESENTATION. No grade, no colour and no remediation
// pointer originates in this file: live rows arrive on the wire already
// graded by the rollup meter, the repo-vantage grid is
// `goat_consistency::grade_intent`, and every pointer is printed VERBATIM in
// a copyable monospace field (§7.6). If a verdict on this page is wrong, the
// bug is in the shared library and the corpus, where one fix reaches every
// surface.
//
// What this file DOES own since the drift-page-polish pass is
// the TRANSLATION layer: every grammar term the page renders goes through one
// human-vocabulary table (`grade_human`), every why line leads with a plain
// conclusion derived from the row's own cells, and every remediation renders
// as a safety label + one action phrase with the exact command behind an
// expansion. Translation, never weakening: the grammar term and the verbatim
// sentence stay on hover titles and expansions, and the API serves the exact
// grammar bytes untouched.
//
// Honesty rules this renderer enforces (§7.5):
//   - the five colours + the grade NAME render together — never a bare
//     green/red dot, and state is text, not a faint tint;
//   - every reading carries its grounding (n over denominator, ages, the
//     current serial) — no percentages, no scores;
//   - an ABSENT rollup renders as "nothing heard", never as healthy, and a
//     stale one says its age instead of posing as current;
//   - no cell is ever blank — a node that has not reported occupies its row
//     and says so.

use crate::{Latest, Siblings, StoredEnvelope};
use goat_consistency::{diff_intents, grade_intent, EnvIntent, GradedRow, Grammar};
use goat_consistency_wire::{ConsistencyDetailV1, EnvDetail, NodeConsistencyV1};
use serde_json::Value;

/// The inlined stylesheet, shared by every page shell this binary serves —
/// one home, so the release view cannot drift into its own look.
pub const STYLE: &str = include_str!("style.css");

pub struct View<'a> {
    pub chosen_site: &'a str,
    pub latest: &'a Latest,
    /// `load_verified`'s result: `(Some(doc), "")` or `(None, refusal)`.
    pub intent: &'a (Option<Value>, String),
    pub siblings: &'a Siblings,
    pub grammar: &'a Grammar,
    pub now_ms: i64,
    pub stale_after_ms: i64,
    pub version: &'a str,
}

pub fn esc(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for c in s.chars() {
        match c {
            '&' => out.push_str("&amp;"),
            '<' => out.push_str("&lt;"),
            '>' => out.push_str("&gt;"),
            '"' => out.push_str("&quot;"),
            '\'' => out.push_str("&#39;"),
            c => out.push(c),
        }
    }
    out
}

/// Percent-encode a query VALUE (Block 94: every value is encoded on
/// construction). Unreserved characters pass; everything else is %XX.
pub fn enc(s: &str) -> String {
    s.bytes()
        .map(|b| match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                (b as char).to_string()
            }
            _ => format!("%{b:02X}"),
        })
        .collect()
}

fn chip(color: &str, label: &str) -> String {
    format!(
        r#"<span class="chip chip-{}">{}</span>"#,
        esc(color),
        esc(label)
    )
}

fn chip_titled(color: &str, label: &str, title: &str) -> String {
    format!(
        r#"<span class="chip chip-{}" title="{}">{}</span>"#,
        esc(color),
        esc(title),
        esc(label),
    )
}

// ---------------------------------------------------------------------------
// Human vocabulary (drift-page-polish). ONE table translates the
// grammar's terms for display — plain words render, the grammar term + its
// meaning ride the hover title, the API and the wire keep the grammar bytes
// untouched. Sweep, not spot-fix: every grade emission on the page goes
// through here. An unknown grade renders as itself — the console does not
// invent vocabulary.
// ---------------------------------------------------------------------------

/// `(plain label, hover title)` for a grammar grade.
pub(crate) fn grade_human(grade: &str) -> (String, String) {
    let (label, meaning) = match grade {
        "match" => ("match", "every compared value agrees"),
        "declared" => ("declared", "a difference somebody declared with a reason — a correct state"),
        "SKEW" => ("undeclared drift", "an undeclared difference — THE drift signal"),
        "unaccountable" => ("unexplained value", "a reported value no generation in the tree explains — not behind, unexplained"),
        "unreadable" => ("unreadable value", "a value is present but could not be evaluated — never a match"),
        "indeterminate" => ("no reading", "nothing to read — never a match, never green"),
        other => (other, "not a grade this console's grammar copy knows"),
    };
    (label.to_string(), format!("grammar grade: {grade} — {meaning}"))
}

/// A grade chip: the plain label on the chip, the grammar term on hover.
fn grade_chip(color: &str, grade: &str) -> String {
    let (label, title) = grade_human(grade);
    format!(
        r#"<span class="chip chip-{} chip-grade" title="{}">{}</span>"#,
        esc(color),
        esc(&title),
        esc(&label),
    )
}

/// An environment-health colour chip (the rollup's own five-state colour, not
/// a grade): plain words, the state term on hover. green/amber/red already
/// speak for themselves; the two ungrounded states get plain names.
fn env_color_chip(color: &str) -> String {
    let (label, meaning) = match color {
        "green" => ("green", "every graded node grounds a healthy reading"),
        "amber" => ("amber", "something needs attention"),
        "red" => ("red", "something is wrong"),
        "idle" => ("no grounded reading", "readings exist but nothing grounds a verdict — never green"),
        "unmetered" => ("no signal", "nothing reported in the window — absence of signal is never health"),
        other => (other, "an environment-health state this console does not know"),
    };
    chip_titled(color, label, &format!("environment-health state: {color} — {meaning}"))
}

fn age_text(ms: i64) -> String {
    if ms < 0 {
        return "age unstatable".into();
    }
    let s = ms / 1000;
    if s >= 86_400 {
        format!("{}d ago", s / 86_400)
    } else if s >= 3_600 {
        format!("{}h ago", s / 3_600)
    } else if s >= 60 {
        format!("{}m ago", s / 60)
    } else {
        format!("{s}s ago")
    }
}

fn short_hash(h: &str) -> String {
    let hex = h.strip_prefix("sha256:").unwrap_or(h);
    let short: String = hex.chars().take(12).collect();
    format!("{short}…")
}

// ---------------------------------------------------------------------------
// Human-first value identity (drift-page-human). The operator's
// rule: machine-readable hashes are NOT shown by default — the default cell
// shows the identity a human compares (the tag, e.g. `sha-e68a6ae`), and the
// full digest + verbatim ref sit behind expansion. Display-only: the API and
// the wire keep the artifact's exact bytes.
// ---------------------------------------------------------------------------

/// The string with `{{ … }}` templating tokens removed — so a literal
/// `{{ ghcr_org }}` (spaces and all, the CF-4 rule) does not disqualify a
/// ref-shaped value from ref-shaped display.
fn without_tmpl_tokens(s: &str) -> String {
    let mut out = String::new();
    let mut rest = s;
    while let Some(i) = rest.find("{{") {
        out.push_str(&rest[..i]);
        match rest[i..].find("}}") {
            Some(j) => rest = &rest[i + j + 2..],
            None => {
                out.push_str(&rest[i..]);
                return out;
            }
        }
    }
    out.push_str(rest);
    out
}

/// Whether `d` is a digest suffix: `sha256:` + hex.
fn is_digest(d: &str) -> bool {
    d.strip_prefix("sha256:")
        .map(|hex| hex.len() >= 4 && hex.chars().all(|c| c.is_ascii_hexdigit()))
        .unwrap_or(false)
}

/// Split an image-ref-shaped value into `(tag, digest)`. `None` when the
/// value is not ref-shaped (a boolean, a group list, a bare hash) — those
/// render as themselves.
fn ref_parts(s: &str) -> Option<(Option<&str>, Option<&str>)> {
    let bare = without_tmpl_tokens(s);
    if !bare.contains('/') || bare.chars().any(|c| c.is_whitespace()) {
        return None;
    }
    let (head, digest) = match s.split_once('@') {
        Some((h, d)) if is_digest(d) => (h, Some(d)),
        Some(_) => return None, // an '@' that is not a digest: not a ref
        None => (s, None),
    };
    let tag = head
        .rfind('/')
        .and_then(|slash| head[slash..].rfind(':').map(|i| &head[slash + i + 1..]))
        .filter(|t| !t.is_empty());
    if tag.is_none() && digest.is_none() {
        return None;
    }
    Some((tag, digest))
}

/// A value's human identity: the tag when the ref carries one, the shortened
/// digest when the digest is all it has.
fn human_identity(tag: Option<&str>, digest: Option<&str>) -> String {
    match (tag, digest) {
        (Some(t), _) => t.to_string(),
        (None, Some(d)) => short_hash(d),
        (None, None) => String::new(),
    }
}

/// A generated sentence, display-shaped: any `sha256:` digest long enough to
/// be machine-only (>= 16 hex) shortens to its first 12 characters. The
/// verbatim sentence stays on the caller's title attribute.
fn shorten_digests(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let mut rest = s;
    while let Some(i) = rest.find("sha256:") {
        let start = i + "sha256:".len();
        let hex_len = rest[start..].chars().take_while(|c| c.is_ascii_hexdigit()).count();
        if hex_len >= 16 {
            out.push_str(&rest[..start + 12]);
            out.push('…');
            rest = &rest[start + hex_len..];
        } else {
            out.push_str(&rest[..start]);
            rest = &rest[start..];
        }
    }
    out.push_str(rest);
    out
}

/// unix ms → `YYYY-MM-DD` (UTC). Hinnant's civil-from-days; no chrono dep.
fn civil_date(ms: i64) -> String {
    let days = ms.div_euclid(86_400_000);
    let z = days + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z.rem_euclid(146_097);
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146_096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = yoe + era * 400 + i64::from(m <= 2);
    format!("{y:04}-{m:02}-{d:02}")
}

/// The action-safety semantics of the grammar's CLOSED remediation set
/// (consistency-foundation.md §6.2, ops/consistency/grammar.yaml), keyed by
/// kind — `(css class, short label, full explanation)`. Display-only
/// vocabulary read off the grammar's own definitions: the SHORT label renders
/// on the chip, the full sentence rides its hover title. Reconverge is the
/// engineering-baseline §2.2 "re-run without fear" guarantee, declare and
/// regenerate change the tree and no host, rollback re-deploys a prior
/// generation, and the three refusing kinds say why they refuse. An unknown
/// kind gets NO label — the console does not invent safety claims.
fn safety_label(kind: &str) -> Option<(&'static str, &'static str, &'static str)> {
    Some(match kind {
        "reconverge" => ("safe", "safe", "safe: an idempotent re-run — the engineering-baseline §2.2 re-run-without-fear guarantee"),
        "regenerate" => ("safe", "safe", "safe: regenerates from the tree — deploys nothing"),
        "declare" => ("policy", "policy edit", "a reviewed PR — changes no host"),
        "rollback" => ("caution", "caution", "re-deploys the prior ledger generation"),
        "restore" => ("refuse", "refuses", "rollback of data is a lie — restore instead"),
        "rotate" => ("refuse", "refuses", "rollback of cert is a lie — rotate re-issues in place"),
        "refuse" => ("refuse", "refuses", "forward-only: no undo path — converge forward"),
        _ => return None,
    })
}

/// The one-line plain action for a remediation kind — what to do, in words an
/// operator digests at a glance. Summarizes the KIND (a closed grammar set),
/// never the pointer: the exact command stays verbatim behind the expansion.
fn action_phrase(kind: &str) -> Option<&'static str> {
    Some(match kind {
        "reconverge" => "re-run the env's refresh play",
        "regenerate" => "regenerate the pins from the tree",
        "declare" => "declare the difference with a reason (pins file, reviewed PR)",
        "rollback" => "roll back to the prior generation",
        "restore" => "restore the data via the component's restore path",
        "rotate" => "rotate the credential in place",
        "refuse" => "converge forward — do not try to undo",
        _ => return None,
    })
}

/// A remediation (§7.6, collapsed by the drift-page-polish pass): default =
/// the safety label + ONE short action phrase; the exact command — verbatim,
/// unshortened, unparameterized, copyable — one click away behind the same
/// expansion pattern as the digests. The console never composes, paraphrases,
/// shortens or executes the command itself (§6.4). An unknown kind renders
/// the long form — kind + verbatim pointer — with no invented summary, and a
/// refusing kind's safety signal is the REFUSES badge alone (one signal, not
/// two).
fn remediation_html(kind: &str, pointer: &str, refuses: bool) -> String {
    if pointer.is_empty() {
        return String::new();
    }
    let label = safety_label(kind);
    let refuse_badge = if refuses {
        let title = label.map(|(_, _, full)| full).unwrap_or("this pointer refuses");
        format!(r#"<span class="refuses" title="{}">REFUSES</span> "#, esc(title))
    } else {
        String::new()
    };
    let safety = match label {
        Some((class, short, full)) if !refuses => format!(
            r#"<span class="rem-safety safety-{class}" title="{}">{}</span> "#,
            esc(full),
            esc(short),
        ),
        _ => String::new(),
    };
    let exact = format!(
        r#"<code class="rem-ptr">{}</code><button class="copy" onclick="cp(this)" title="copy the exact command">copy</button>"#,
        esc(pointer),
    );
    match action_phrase(kind) {
        Some(phrase) => format!(
            r#"<div class="rem">{refuse_badge}{safety}<span class="rem-action">{}</span><details class="rem-more"><summary>exact command</summary><div class="rem-exact">{exact}</div></details></div>"#,
            esc(phrase),
        ),
        None => format!(
            r#"<div class="rem">{refuse_badge}<span class="rem-kind">{}</span> {exact}</div>"#,
            esc(kind),
        ),
    }
}

/// A value for a table cell, display-only transformed: HTML-escaped, then any
/// literal `{{ var }}` templating token the artifact carries (the CF-4
/// ghcr_org rule keeps declared values verbatim, braces and all) renders as a
/// subdued `<var>` token so it reads as "a placeholder", not as noise. The
/// verbatim string stays on the title attribute and on the API — this is the
/// portal-lexicon display-only precedent: what the machine holds is untouched,
/// only the pixels change.
fn display_value_html(s: &str) -> String {
    let escaped = esc(s);
    let mut out = String::with_capacity(escaped.len());
    let mut rest = escaped.as_str();
    while let Some(i) = rest.find("{{") {
        out.push_str(&rest[..i]);
        let after = &rest[i + 2..];
        let replaced = match after.find("}}") {
            Some(j) => {
                let inner = after[..j].trim();
                let is_ident = !inner.is_empty()
                    && inner
                        .chars()
                        .all(|c| c.is_ascii_alphanumeric() || c == '_' || c == '-' || c == '.');
                if is_ident {
                    out.push_str(&format!(
                        r#"<span class="tmpl" title="verbatim in the artifact: {{{{ {inner} }}}}">&lt;{inner}&gt;</span>"#
                    ));
                    rest = &after[j + 2..];
                    true
                } else {
                    false
                }
            }
            None => false,
        };
        if !replaced {
            // Not a simple templating token: keep the braces literally.
            out.push_str("{{");
            rest = after;
        }
    }
    out.push_str(rest);
    out
}

/// One cell value for display. A ref-shaped value (an image ref) renders its
/// HUMAN identity by default — the tag, or the shortened digest when a tag is
/// all it lacks — with the full verbatim ref one click away behind a
/// `<details>`. A long non-ref value gets the SAME expansion affordance so a
/// grid cell stays one line (the drift-page-grid rule). `absent` renders as a
/// single dimmed word — the chip and the section heading already say
/// "declared", so the cell carries no vocabulary, only the state.
/// Display-only, per the portal-lexicon precedent: the API and the artifact
/// keep the exact bytes.
fn display_cell_html(text: &str) -> String {
    if text == "absent" {
        return r#"<span class="dim">absent</span>"#.to_string();
    }
    // An empty resolved value is a value (a deny-all door, an empty list) —
    // quoted so the cell is never a blank.
    if text.is_empty() {
        return r#"<span class="dim" title="the artifact's value is the empty string">""</span>"#
            .to_string();
    }
    // The diff's env-level "not governed" statement, shortened for the cell —
    // the full library sentence stays on the title attribute (and the row's
    // conclusion names the env in words).
    if text.starts_with("unset: not governed") {
        return format!(r#"<span class="dim" title="{}">not governed here</span>"#, esc(text));
    }
    match ref_parts(text) {
        Some((tag, digest)) => format!(
            r#"<details class="ref"><summary>{}</summary><code class="ref-full">{}</code></details>"#,
            esc(&human_identity(tag, digest)),
            display_value_html(text),
        ),
        // A long non-ref value (a group list, a config blob): the cell shows
        // the one-line truncation, the exact value one click away.
        None if text.chars().count() > 24 => format!(
            r#"<details class="ref"><summary title="{}">{}</summary><code class="ref-full">{}</code></details>"#,
            esc(text),
            esc(text),
            display_value_html(text),
        ),
        None => display_value_html(text),
    }
}

/// The value cells of a grid row — one `gval` span per env column, one line
/// each, right-set by the stylesheet. When every column carries the IDENTICAL
/// text, the value renders ONCE across a spanned cell ("all: <value>") — an
/// agreement needs one statement, not one per column. Only differing rows get
/// per-column cells.
fn value_cells_html(columns: &[String], cells: &std::collections::BTreeMap<String, Value>) -> String {
    let texts: Vec<String> = columns
        .iter()
        .map(|c| match cells.get(c) {
            Some(cell) => cell_text(cell),
            // A synthetic env-level cell (key `<env>` with no member — the
            // diff's "not governed here" statement) folds into that env's
            // member columns at display time, instead of minting a phantom
            // column of "no column" filler. Column-folding only; the cell's
            // own text renders untouched.
            None => match c.split_once(':').and_then(|(env, _)| cells.get(env)) {
                Some(cell) => cell_text(cell),
                None => "no column".to_string(),
            },
        })
        .collect();
    if columns.len() > 1 && texts.windows(2).all(|w| w[0] == w[1]) {
        format!(
            r#"<span class="gval gval-agree mono"><span class="dim">all:</span> {}</span>"#,
            display_cell_html(&texts[0]),
        )
    } else {
        texts
            .iter()
            .map(|t| format!(r#"<span class="gval mono">{}</span>"#, display_cell_html(t)))
            .collect()
    }
}

// ---------------------------------------------------------------------------
// Conclusion-first why lines (drift-page-polish). The default why
// line is the CONCLUSION in plain words — who differs, who lacks the thing,
// and (when declared) the declarer's own reason. The library's generated
// env=value enumeration is not lost: it renders verbatim (digests shortened,
// tokens subdued; the exact sentence on the title attribute and untouched on
// the API) behind the same one-click expansion the digests use. Translation,
// not weakening — no verdict originates here, only the words.
// ---------------------------------------------------------------------------

/// A column name in human words: `net:net_b_hosts` → `net/net-b`; a
/// member group whose name restates its env collapses to the env alone
/// (`net:net_hosts` → `net`).
fn col_human(col: &str) -> String {
    let Some((env, member)) = col.split_once(':') else {
        return col.replace('_', "-");
    };
    let short = member.trim_end_matches("_hosts").replace('_', "-");
    if short == env || short == env.replace('_', "-") {
        env.to_string()
    } else {
        format!("{env}/{short}")
    }
}

/// The first sentence of a declaration reason (for the at-a-glance line) and
/// whether the reason carries more than that.
fn first_sentence(reason: &str) -> (&str, bool) {
    match reason.find(". ") {
        Some(i) => (&reason[..i], true),
        None => (reason.trim_end().trim_end_matches('.'), false),
    }
}

/// Row-level declared reasons, `(subject, name) → reason`, read off a
/// verified intent document. The artifact carries TWO declaredness shapes:
/// cell-level markers (`members.<m>.declared`, the components shape) and
/// row-level maps (`declared: {<member>: {reason}}`, the capabilities shape).
/// The VERDICT for both comes from the grading library (declared_scopes);
/// this map is presentation-only — it lets the page print the declarer's own
/// words for rows whose cells carry no reason of their own.
fn row_reasons(doc: &Value) -> std::collections::BTreeMap<(String, String), String> {
    let mut out = std::collections::BTreeMap::new();
    for (section, key, subject) in
        [("components", "name", "component"), ("capabilities", "var", "capability")]
    {
        let Some(rows) = doc.get(section).and_then(|v| v.as_array()) else { continue };
        for row in rows {
            let Some(name) = row.get(key).and_then(|v| v.as_str()) else { continue };
            let Some(decl) = row.get("declared").and_then(|v| v.as_object()) else { continue };
            if let Some(r) =
                decl.values().filter_map(|d| d.get("reason").and_then(|r| r.as_str())).next()
            {
                out.entry((subject.to_string(), name.to_string()))
                    .or_insert_with(|| r.to_string());
            }
        }
    }
    out
}

/// One non-match grid row's plain conclusion, derived from its own cells:
/// `(sentence, full declaration reason when the sentence truncated it)`.
/// `fallback_reason` is the row-level declared reason (see `row_reasons`),
/// consulted only when no cell carries one.
fn row_conclusion(row: &GradedRow, fallback_reason: Option<&str>) -> (String, Option<String>) {
    let mut absent: Vec<String> = Vec::new();
    let mut resolved: std::collections::BTreeMap<String, Vec<String>> = Default::default();
    let mut other: Vec<(String, String)> = Vec::new();
    let mut reason: Option<String> = None;
    for (col, cell) in &row.cells {
        if reason.is_none() {
            if let Some(r) =
                cell.get("declared").and_then(|d| d.get("reason")).and_then(|r| r.as_str())
            {
                reason = Some(r.to_string());
            }
        }
        match cell.get("state").and_then(|s| s.as_str()).unwrap_or("") {
            "absent" => absent.push(col_human(col)),
            "resolved" => {
                let v = match cell.get("value") {
                    Some(Value::String(s)) => s.clone(),
                    Some(o) => o.to_string(),
                    None => String::new(),
                };
                resolved.entry(v).or_default().push(col_human(col));
            }
            _ => other.push((col_human(col), cell_text(cell))),
        }
    }
    let reason = reason.or_else(|| fallback_reason.map(str::to_string));
    let list = |v: &[String]| v.join(" + ");
    let (reason_lead, full_reason) = match &reason {
        Some(r) => {
            let (s, truncated) = first_sentence(r);
            (Some(s.to_string()), truncated.then(|| r.clone()))
        }
        None => (None, None),
    };
    let name = &row.name;
    // Vocabulary dedup (drift-page-grid): the row's status appears ONCE — on
    // the chip. The sentence carries the substance (who differs, who lacks
    // the thing, the declarer's reason), never a repeated status word.
    let sentence = match row.grade.as_str() {
        "declared" => {
            let why = reason_lead.unwrap_or_else(|| "a reason is on record".to_string());
            if !absent.is_empty() && !resolved.is_empty() {
                format!("{} doesn't run {name} — {why}", list(&absent))
            } else if resolved.len() > 1 {
                format!("the values deliberately differ — {why}")
            } else {
                why
            }
        }
        "SKEW" => {
            if !absent.is_empty() && !resolved.is_empty() {
                format!("{} doesn't have {name}, and nothing declares why", list(&absent))
            } else {
                let mut groups: Vec<&Vec<String>> = resolved.values().collect();
                groups.sort_by_key(|cols| std::cmp::Reverse(cols.len()));
                if groups.len() == 2 && groups[0].len() > groups[1].len() {
                    format!("{} differs from the rest, and nothing declares why", list(groups[1]))
                } else {
                    "the values differ, and nothing declares why".to_string()
                }
            }
        }
        "unaccountable" => "a reported value no generation in the tree explains".to_string(),
        "unreadable" => {
            let cols: Vec<String> = other.iter().map(|(c, _)| c.clone()).collect();
            if cols.is_empty() {
                "a value is present but can't be evaluated".to_string()
            } else {
                format!("{}'s value can't be evaluated", list(&cols))
            }
        }
        "indeterminate" => {
            if let Some((_, text)) = other.iter().find(|(_, t)| t.contains("not governed")) {
                let detail = text.split_once(": ").map(|(_, d)| d).unwrap_or(text);
                // First clause only — "no entry to compare" restates the verdict.
                let lead = detail.split(" — ").next().unwrap_or(detail);
                format!("not comparable — {lead}")
            } else if !other.is_empty() && !resolved.is_empty() {
                let cols: Vec<String> = other.iter().map(|(c, _)| c.clone()).collect();
                format!("nothing to read for {}", list(&cols))
            } else {
                "nothing to read".to_string()
            }
        }
        _ => why_clause_human(&row.why),
    };
    (sentence, full_reason)
}

/// A wire row's plain conclusion, translated from the library's generated
/// clause (the sentence tail after the enumeration) — wire rows carry no
/// cells, so the clause is all there is to translate.
fn why_clause_human(why: &str) -> String {
    let clause = why.rsplit(" — ").next().unwrap_or(why);
    match clause {
        "all agree" => "all agree".to_string(),
        // No repeated status word — the chip already says "declared".
        "the difference is declared with a reason" => "a deliberate difference".to_string(),
        "they differ and nothing declares why" => {
            "observed and expected differ, and nothing declares why".to_string()
        }
        "a reported value is explained by no generation in the tree" => {
            "the node reports a value no generation in the tree explains".to_string()
        }
        "read, not evaluable" => "a value is present but can't be evaluated".to_string(),
        "there was nothing to read" => "nothing to read".to_string(),
        "nothing reported" => "nothing reported".to_string(),
        other => other.to_string(),
    }
}

/// The why line, conclusion-first: the plain sentence renders; the library's
/// verbatim enumeration (display-shaped: digests shortened, tokens subdued;
/// the exact sentence on the title attribute) sits behind the expansion,
/// together with the full text of a truncated declaration reason.
fn display_why_html(why: &str, conclusion: &str, full_reason: Option<&str>) -> String {
    let reason_html = full_reason
        .map(|r| format!(r#"<div class="why-reason">{}</div>"#, esc(r)))
        .unwrap_or_default();
    format!(
        r#"<details class="why"><summary>{}</summary><div class="why-full" title="{}">{}</div>{reason_html}</details>"#,
        esc(conclusion),
        esc(why),
        display_value_html(&shorten_digests(why)),
    )
}

/// The full-width explanation sub-row beneath a non-match NODE-TABLE row:
/// why and remediation move OUT of the column grid and span the table, so a long
/// verbatim pointer wraps across the page width instead of character-per-line
/// down a narrow trailing column. `extra` is the drift table's distance line
/// (empty everywhere else), rendered between the why and the pointer —
/// consistent / why / how far / what to run, in the operator's own order.
#[allow(clippy::too_many_arguments)]
fn detail_subrow(
    colspan: usize,
    sev: &str,
    why: &str,
    conclusion: &str,
    full_reason: Option<&str>,
    kind: &str,
    ptr: &str,
    refuses: bool,
    extra: &str,
) -> String {
    format!(
        r#"<tr class="rowdetail sev-{}"><td colspan="{colspan}">{}{extra}{}</td></tr>"#,
        esc(sev),
        display_why_html(why, conclusion, full_reason),
        remediation_html(kind, ptr, refuses),
    )
}

/// The bound explanation block under a grid row's cells (drift-page-grid):
/// indented under the subject column, measure-capped by the stylesheet
/// (~90ch), inside the row block so the severity edge spans cells + sentence
/// as one unit.
fn gdetail_html(
    why: &str,
    conclusion: &str,
    full_reason: Option<&str>,
    kind: &str,
    ptr: &str,
    refuses: bool,
    extra: &str,
) -> String {
    format!(
        r#"<div class="gdetail">{}{extra}{}</div>"#,
        display_why_html(why, conclusion, full_reason),
        remediation_html(kind, ptr, refuses),
    )
}

/// A grid row's cell line: [subject+name] [grade chip] [one value per env
/// column]. One CSS-grid line whose tracks are fixed by the stylesheet, so
/// every row aligns with the sticky header above it.
fn gcells_html(row: &GradedRow, color: &str, columns: &[String]) -> String {
    format!(
        r#"<div class="gcells"><span class="gid mono" title="{} {}">{} <span class="gsubj">{}</span></span><span class="ggrade">{}</span>{}</div>"#,
        esc(&row.subject),
        esc(&row.name),
        esc(&row.name),
        esc(&row.subject),
        grade_chip(color, &row.grade),
        value_cells_html(columns, &row.cells),
    )
}

/// One GradedRow as HTML — shared by the repo-vantage grid and the cross-env
/// drift table (layout only; the verdict arrived graded). Density tiers
/// (drift-page-grid): a `match` row is one grid-aligned line with no
/// explanation; a `declared` row is ONE line by default — its sentence and
/// remediation sit behind the row's own disclosure (presentation, never
/// omission); every other grade renders expanded — cells, sentence and action
/// visible — with its severity edge spanning the whole row block.
fn grid_row_html(
    row: &GradedRow,
    color: &str,
    columns: &[String],
    fallback_reason: Option<&str>,
) -> String {
    grid_row_html_extra(row, color, columns, "", fallback_reason)
}

/// `grid_row_html` with an extra detail line (the drift table's distance).
fn grid_row_html_extra(
    row: &GradedRow,
    color: &str,
    columns: &[String],
    extra: &str,
    fallback_reason: Option<&str>,
) -> String {
    let cells = gcells_html(row, color, columns);
    if row.grade == "match" {
        return format!(r#"<div class="grow row-match">{cells}</div>"#);
    }
    let (kind, ptr, refuses) = row
        .remediation
        .as_ref()
        .map(|r| (r.kind.as_str(), r.pointer.as_str(), r.refuses))
        .unwrap_or(("", "", false));
    let (conclusion, full_reason) = row_conclusion(row, fallback_reason);
    let detail =
        gdetail_html(&row.why, &conclusion, full_reason.as_deref(), kind, ptr, refuses, extra);
    if row.grade == "declared" {
        format!(
            r#"<details class="grow gdecl sev-{}"><summary>{cells}</summary>{detail}</details>"#,
            esc(color),
        )
    } else {
        format!(r#"<div class="grow has-detail sev-{}">{cells}{detail}</div>"#, esc(color))
    }
}

/// A grid section's sticky header line: the fixed identity + grade tracks,
/// then one header per env column — the human name visible, the full
/// `<goatnet>:<member>` identity on hover, sticky (with background) so values
/// never lose their column meaning while scrolling.
fn ghead_html(columns: &[String], human: bool) -> String {
    let mut out = String::from(r#"<div class="ghead"><span class="gh">subject</span><span class="gh">grade</span>"#);
    for c in columns {
        let display = if human { col_human(c) } else { c.clone() };
        out.push_str(&format!(
            r#"<span class="gh gh-env mono" title="{}">{}</span>"#,
            esc(c),
            esc(&display),
        ));
    }
    out.push_str("</div>");
    out
}

/// Site page -> the cross-env comparison page.
///
/// A site console sees ONE goatnet: "how does this compare to the other
/// environment" is the question it structurally cannot answer, and until this
/// existed an operator had to know a second URL by heart. Hidden, never dead,
/// when unset — the §7.7 rule.
fn crossenv_link(v: &View) -> String {
    match &v.siblings.crossenv {
        Some(base) => format!(
            r#" &middot; <a href="{}" title="compare this goatnet against the others — the one question a single-site page cannot answer">cross-env drift &rarr;</a>"#,
            esc(base)
        ),
        None => String::new(),
    }
}

/// Comparison page -> a goatnet's OWN site console.
///
/// Not a link to missing information: this page already renders every site's
/// full grid. It is the link to the copy that SURVIVES ISOLATION (§7.9) — when
/// the WAN is cut this page is unreachable and that one still answers for its
/// own site. Worth saying on the link itself, because "why would I click
/// through to a subset" is the obvious question.
fn console_link(cfg_url: Option<&str>) -> String {
    match cfg_url {
        Some(u) if !u.is_empty() => format!(
            r#" <a class="sib" href="{}" title="this goatnet's own console — the copy that still answers when the WAN is cut (island mode)">site console &#8599;</a>"#,
            esc(u)
        ),
        _ => String::new(),
    }
}

fn sibling_links_band(v: &View, site: &str) -> String {
    let mut out = String::new();
    // "why is it red" — the ADR 1036 hop toward a stethoscope environment
    // target. Hidden, not dead, when the sibling is unset.
    if let Some(base) = &v.siblings.stethoscope {
        out.push_str(&format!(
            r#"<a class="sib" href="{}/?domain=consistency&amp;site={}" title="this domain's environment target in stethoscope">stethoscope &#8599;</a>"#,
            esc(base),
            enc(site)
        ));
    }
    out
}

fn sibling_link_peer(v: &View, host: &str, site: &str) -> String {
    match &v.siblings.panoscope {
        Some(base) => format!(
            r#"<a class="sib" href="{}/?peer={}&amp;site={}" title="what is this node doing right now, in panoscope">panoscope &#8599;</a>"#,
            esc(base),
            enc(host),
            enc(site)
        ),
        None => String::new(),
    }
}

// ---------------------------------------------------------------------------
// Sections
// ---------------------------------------------------------------------------

fn section_intent(v: &View) -> String {
    match v.intent {
        (Some(doc), _) => {
            let get = |k: &str| doc.get(k).and_then(|x| x.as_str()).unwrap_or("");
            let serial = doc.get("serial").and_then(|x| x.as_u64()).unwrap_or(0);
            let members: Vec<String> = doc
                .get("members")
                .and_then(|m| m.as_array())
                .map(|a| a.iter().filter_map(|x| x.as_str().map(String::from)).collect())
                .unwrap_or_default();
            format!(
                r#"<section class="panel">
<h2>committed intent <span class="ok-badge">signature verified</span></h2>
<table class="kv">
<tr><th>goatnet</th><td class="mono">{}</td></tr>
<tr><th>generation</th><td class="mono">serial {} &middot; {}</td></tr>
<tr><th>content hash</th><td class="mono" title="{}">{}</td></tr>
<tr><th>members</th><td class="mono">{}</td></tr>
</table>
</section>"#,
                esc(get("goatnet")),
                serial,
                esc(get("generated_at")),
                esc(get("content_hash")),
                esc(&short_hash(get("content_hash"))),
                esc(&members.join(", ")),
            )
        }
        (None, refusal) => format!(
            r#"<section class="panel refusal">
<h2>committed intent — REFUSED</h2>
<p class="refusal-text">{}</p>
<p>The console refuses to grade from an unverified artifact: the committed-intent
grid is not rendered. The live rollup below, if any, is the rollup meter's own
reading against <em>its</em> copy of the artifact and carries its own
verification state.</p>
</section>"#,
            esc(refusal)
        ),
    }
}

/// The coverage state as a badge beside the grade — ADR 1125's third answer,
/// rendered as its OWN thing rather than folded into the grade chip. An
/// `observed` row gets nothing: the badge exists to name the rows that are not
/// comparisons, and badging the ordinary case would make the exception
/// invisible again.
///
/// The words are the grammar's state names verbatim; this renderer picks no
/// synonyms and invents no fifth state (an unrecognised value renders as
/// itself, because a state this console does not know about is a fact about
/// the publisher, not something to swallow).
fn coverage_badge(coverage: &str) -> String {
    if coverage.is_empty() || coverage == "observed" {
        return String::new();
    }
    let title = match coverage {
        "no-instrument" => "coverage: no-instrument — governed, and nothing declares how a node could prove it. Re-running the play cannot fix this; declaring an instrument can.",
        "silent" => "coverage: silent — an instrument exists and this node has not reported through it.",
        "unreported-scope" => "coverage: unreported-scope — intent governs this member and no node claims it.",
        _ => "coverage state published by the node, unknown to this console — rendered verbatim rather than swallowed",
    };
    format!(
        r#" <span class="chip chip-unmetered cov-badge" title="{}">{}</span>"#,
        esc(title),
        esc(coverage),
    )
}

/// The per-node coverage count, with its denominator implied by the row list
/// beside it. Zero is STATED rather than omitted — a category that disappears
/// when it is empty reads as a category that does not exist, which is how the
/// gap this replaced stayed invisible.
fn coverage_summary(gaps: u32) -> String {
    if gaps == 0 {
        r#"<span title="every row on this node is a real comparison">no coverage gaps</span>"#
            .to_string()
    } else {
        format!(
            r#"<strong title="rows that are not comparisons: nothing observes them. Counted beside the grade, never folded into it (ADR 1125).">{gaps} coverage gap(s)</strong>"#
        )
    }
}

#[cfg(test)]
pub fn coverage_badge_for_test(coverage: &str) -> String {
    coverage_badge(coverage)
}

#[cfg(test)]
pub fn coverage_summary_for_test(gaps: u32) -> String {
    coverage_summary(gaps)
}

fn node_rows_html(v: &View, node: &NodeConsistencyV1, site: &str) -> String {
    let mut rows = String::new();
    for r in &node.rows {
        // Same structure as the grid tables: a match row is one compact line
        // (observed == expected, stated once); a non-match row keeps the
        // observed → expected pair and carries its why + remediation in a
        // full-width sub-row instead of narrow trailing columns.
        let value_cell = if r.grade == "match" || r.observed == r.expected {
            format!(
                r#"<td class="mono dim" title="observed {}">{}</td>"#,
                esc(&r.observed),
                esc(&short_or_dash(&r.observed)),
            )
        } else {
            format!(
                r#"<td class="mono dim" title="observed {} / expected {}">{} &rarr; {}</td>"#,
                esc(&r.observed),
                esc(&r.expected),
                esc(&short_or_dash(&r.observed)),
                esc(&short_or_dash(&r.expected)),
            )
        };
        // ADR 1125: a coverage row is not a comparison, and saying so on the
        // row is the whole change — the alternative is what it replaced, which
        // was the row not existing at all.
        let cov = coverage_badge(&r.coverage);
        let head_cells = format!(
            r#"<td class="mono">{}</td><td class="mono">{}</td><td>{}{}</td>{}"#,
            esc(&r.subject),
            esc(&r.name),
            grade_chip(&r.color, &r.grade),
            cov,
            value_cell,
        );
        if r.grade == "match" {
            rows.push_str(&format!(r#"<tr class="row-match">{head_cells}</tr>"#));
        } else {
            rows.push_str(&format!(
                r#"<tr class="has-detail sev-{}">{head_cells}</tr>{}"#,
                esc(&r.color),
                detail_subrow(
                    4,
                    &r.color,
                    &r.why,
                    &why_clause_human(&r.why),
                    None,
                    &r.remediation_kind,
                    &r.remediation,
                    r.remediation_refuses,
                    "",
                ),
            ));
        }
    }
    let heard = v
        .latest
        .reports
        .get(&node.host_id)
        .map(|s| format!("record heard {}", age_text(v.now_ms - s.received_unix_ms)))
        .unwrap_or_else(|| "no raw record heard by this console (drill unavailable)".to_string());
    format!(
        r#"<article class="node">
<h3><span class="mono">{}</span> <span class="dim">({})</span> {} {}</h3>
<p class="grounding">generation: serial {} &middot; {} behind &middot; intent age {} &middot;
components {} of {} observed &middot; renders {} of {} recorded &middot; {} &middot;
report age {} &middot; stamp signature {}</p>
<table class="rows">
<thead><tr><th>subject</th><th>name</th><th>grade</th><th>observed &rarr; expected</th></tr></thead>
<tbody>{}</tbody>
</table>
<details class="drill" data-host="{}" ontoggle="drill(this)">
<summary>raw record ({})</summary>
<pre class="drill-body">open to fetch /api/node/{}</pre>
</details>
</article>"#,
        esc(&node.host_id),
        esc(&node.member_group),
        grade_chip(&node.color, &node.grade),
        sibling_link_peer(v, &node.host_id, site),
        node.intent_serial,
        node.behind_by,
        if node.intent_age_seconds >= 0 {
            age_text(node.intent_age_seconds * 1000)
                .trim_end_matches(" ago")
                .to_string()
        } else {
            "unstatable".to_string()
        },
        node.components_observed,
        node.components_governed,
        node.renders_observed,
        node.renders_declared,
        coverage_summary(node.coverage_gaps),
        age_text(node.report_age_ms),
        if node.intent_verified {
            "verified"
        } else {
            "FAILED verification — treated as unreadable, never as a match"
        },
        rows,
        esc(&node.host_id),
        esc(&heard),
        esc(&node.host_id),
    )
}

/// A wire-carried observed/expected value, display-shortened: a ref-shaped
/// value shows its human identity (tag first, digest shortened), a bare hash
/// its first 12 characters, an empty value a dash. The full string stays on
/// the cell's title attribute.
fn short_or_dash(s: &str) -> String {
    if s.is_empty() {
        return "—".to_string();
    }
    match ref_parts(s) {
        Some((tag, digest)) => human_identity(tag, digest),
        None => short_hash(s),
    }
}

pub(crate) fn detail_of(env: &goat_consistency_wire::EnvHealthV1) -> Option<&ConsistencyDetailV1> {
    match &env.detail {
        Some(EnvDetail::Consistency(d)) => Some(d),
        None => None,
    }
}

fn section_rollup(v: &View, scope: &str, stored: &StoredEnvelope) -> String {
    let env = &stored.env;
    let reading_age = v.now_ms - stored.received_unix_ms;
    let stale = reading_age > v.stale_after_ms;
    let d = match detail_of(env) {
        Some(d) => d,
        None => {
            return format!(
                r#"<section class="panel"><h2>{}</h2><p>{} an envelope arrived on this scope with no consistency arm — it could be read but not evaluated; nothing is graded from it.</p></section>"#,
                esc(scope),
                chip_titled(
                    "idle",
                    "unreadable envelope",
                    "grammar grade: unreadable — read, not evaluable; never a match",
                ),
            )
        }
    };
    let mut out = String::new();
    let head_chip = if stale {
        chip("idle", &format!("stale — last heard {}", age_text(reading_age)))
    } else {
        env_color_chip(&env.overall_color)
    };
    out.push_str(&format!(
        r#"<section class="panel">
<h2>live rollup — <span class="mono">{}</span> {} {}</h2>
<p class="grounding">{} of {} expected nodes reporting (window {}s) &middot; graded against serial {} ({}) &middot;
skew band: {} generation(s), {}s &middot; reading published {} &middot; heard by this console {}</p>"#,
        esc(scope),
        head_chip,
        sibling_links_band(v, scope),
        d.nodes_reporting,
        d.nodes_expected,
        env.window_secs,
        d.current_serial,
        esc(&short_or_dash(&d.current_content_hash)),
        d.skew_band_generations,
        d.skew_band_seconds,
        age_text(v.now_ms - env.ts_unix_ms),
        age_text(reading_age),
    ));
    if stale {
        out.push_str(&format!(
            r#"<p class="stale-note">This reading is older than the staleness window ({}s): it is shown as
history, not as the current state. The rollup meter may be down, its write may be
silently denied, or this console's session may have dropped.</p>"#,
            v.stale_after_ms / 1000
        ));
    }
    if !d.intent_readable {
        out.push_str(&format!(
            r#"<p class="refusal-text">The rollup meter itself REFUSED to grade: {}</p>"#,
            esc(&d.intent_note)
        ));
    }
    // ADR 1125 `unreported-scope`. `N of M reporting` above says how many
    // members are missing; this says WHICH. The distinction is the whole F-324
    // lesson — a governed host nobody looked at is invisible inside a ratio,
    // and the ratio reads as a rounding error rather than as a host.
    if !d.unreported_members.is_empty() {
        // The pointer comes from the grammar, like every other one — asked for
        // by name, printed verbatim, never written here (§6.4). `enrol` was
        // declared for this state and had nothing emitting it until now.
        let pointer = v
            .grammar
            .coverage_remediation("unreported-scope")
            .ok()
            .flatten()
            .and_then(|kind| {
                v.grammar
                    .pointer(kind, &[("member", &d.unreported_members.join(", ")), ("name", "")])
                    .ok()
            })
            .map(|r| format!(r#"<p class="rem-ptr">{}</p>"#, esc(&r.pointer)))
            .unwrap_or_default();
        out.push_str(&format!(
            r#"<p class="grounding"><strong>governed and unreported:</strong> {} — {} member(s) this goatnet's intent governs that NO node claimed. Not a slow node: nothing reports for them at all, so every row they would carry is absent rather than pending.</p>{}"#,
            d.unreported_members
                .iter()
                .map(|m| format!(r#"<code>{}</code>"#, esc(m)))
                .collect::<Vec<_>>()
                .join(", "),
            d.unreported_members.len(),
            pointer,
        ));
    }
    if d.nodes.is_empty() {
        out.push_str(&format!(
            r#"<p>{} no node graded in this reading — {} node(s) reported within the window.
Absence of a grade is not health.</p>"#,
            env_color_chip(&env.overall_color),
            d.nodes_reporting
        ));
    }
    for node in &d.nodes {
        out.push_str(&node_rows_html(v, node, scope));
    }
    out.push_str("</section>");
    out
}

fn section_nothing_heard(v: &View) -> String {
    format!(
        r#"<section class="panel">
<h2>live rollup — <span class="mono">{}</span> {}</h2>
<p><strong>nothing heard</strong> on <code>ops/consistency/health/{}</code>.
That is an answer, not a blank: the rollup meter is not deployed or not running,
its write is being silently denied by the router ACL, or this console
cannot reach the router. It is never rendered as healthy.</p>
<p class="grounding">console bus state: {}</p>
</section>"#,
        esc(v.chosen_site),
        chip("unmetered", "nothing heard"),
        esc(v.chosen_site),
        esc(if v.latest.bus_note.is_empty() { "session not yet opened" } else { &v.latest.bus_note }),
    )
}

/// The repo-vantage subject grid: components + capabilities across the
/// goatnet's member columns, graded by the library from the verified artifact
/// alone. Only rendered when verification passed — §7's verify-at-read rule.
fn section_repo(v: &View) -> String {
    let Some(doc) = &v.intent.0 else { return String::new() };
    let rows: Vec<GradedRow> = match grade_intent(doc, "repo", v.grammar) {
        Ok(r) => r,
        Err(e) => {
            return format!(
                r#"<section class="panel refusal"><h2>committed-intent grid — REFUSED</h2>
<p class="refusal-text">the grading library refused: {}</p></section>"#,
                esc(&e.to_string())
            )
        }
    };
    let members: Vec<String> = doc
        .get("members")
        .and_then(|m| m.as_array())
        .map(|a| a.iter().filter_map(|x| x.as_str().map(String::from)).collect())
        .unwrap_or_default();

    let head = ghead_html(&members, false);
    let reasons = row_reasons(doc);
    let mut body = String::new();
    for row in &rows {
        let color = v.grammar.color(&row.grade, false).unwrap_or_else(|_| "idle".into());
        let fallback = reasons.get(&(row.subject.clone(), row.name.clone()));
        body.push_str(&grid_row_html(row, &color, &members, fallback.map(String::as_str)));
    }
    format!(
        r#"<section class="panel">
<h2>committed-intent grid <span class="dim">(repo vantage — what the tree says, no host consulted)</span></h2>
<div class="grid" style="--envn:{}">{}{}</div>
</section>"#,
        members.len(),
        head,
        body
    )
}

/// A member cell of the artifact, as text. States are answers, not filler.
fn cell_text(cell: &Value) -> String {
    let state = cell.get("state").and_then(|s| s.as_str()).unwrap_or("");
    match state {
        "resolved" => {
            let val = match cell.get("value") {
                Some(Value::String(s)) => s.clone(),
                Some(other) => other.to_string(),
                None => "resolved (no value)".to_string(),
            };
            // The full value — display shaping (tag-first, digest behind
            // expansion) happens in display_cell_html, never here, so the
            // agree-collapse compares exact values. No `[declared]` marker:
            // the status appears ONCE per row, on the chip (drift-page-grid
            // vocabulary dedup) — a cell shows the value only.
            val
        }
        "absent" => "absent".to_string(),
        "" => "no state".to_string(),
        other => match cell.get("detail").and_then(|d| d.as_str()) {
            Some(d) => format!("{other}: {d}"),
            None => other.to_string(),
        },
    }
}

// ---------------------------------------------------------------------------
// The page
// ---------------------------------------------------------------------------

/// One goatnet's full section stack — committed intent, live rollup (or its
/// honest absence), extra heard scopes, repo-vantage grid. THE code path:
/// single mode calls it once, the CF-7 aggregator calls it once per
/// configured goatnet, so the aggregator cannot develop its own semantics
/// (§7.8 constraint 1).
pub fn goatnet_sections(v: &View) -> String {
    let mut sections = String::new();
    sections.push_str(&section_intent(v));

    // The chosen scope first — its absence is a rendered answer.
    match v.latest.rollups.get(v.chosen_site) {
        Some(stored) => sections.push_str(&section_rollup(v, v.chosen_site, stored)),
        None => sections.push_str(&section_nothing_heard(v)),
    }
    // Anything else this console can hear (a second goatnet's rollup crossing
    // a shared router) renders too — worth seeing, never the subject.
    for (scope, stored) in &v.latest.rollups {
        if scope != v.chosen_site {
            sections.push_str(&section_rollup(v, scope, stored));
        }
    }

    sections.push_str(&section_repo(v));
    sections
}

pub fn render_page(v: &View) -> String {
    let sections = goatnet_sections(v);

    format!(
        r#"<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="30">
<title>consistency — {}</title>
<style>{}</style>
</head>
<body>
<header>
<h1>consistency <span class="mono">{}</span></h1>
<p class="tagline">is every node running what it should, where is it not, and what do I run to fix it?
&mdash; a stateless projection; this page executes nothing. <a href="/release">release readiness &rarr;</a>{}</p>
</header>
{}
<footer>
<p class="dim">goat-consistency-console v{} &middot; ADR 1110 D5 &middot; grades and pointers come from the shared
grammar (ops/consistency/grammar.yaml) — this page renders them and computes nothing.
No percentages by design: counts with denominators, or nothing.</p>
</footer>
<script>{}</script>
</body>
</html>"#,
        esc(v.chosen_site),
        STYLE,
        esc(v.chosen_site),
        crossenv_link(v),
        sections,
        esc(v.version),
        PAGE_SCRIPT,
    )
}

/// The page's only JavaScript: the copy affordance and the node drill. Shared
/// verbatim by both page shells.
const PAGE_SCRIPT: &str = r#"
function cp(b){var t=b.previousElementSibling.textContent;
if(navigator.clipboard&&navigator.clipboard.writeText){navigator.clipboard.writeText(t).then(function(){b.textContent='copied';setTimeout(function(){b.textContent='copy';},1200);});}
else{var ta=document.createElement('textarea');ta.value=t;document.body.appendChild(ta);ta.select();document.execCommand('copy');document.body.removeChild(ta);b.textContent='copied';setTimeout(function(){b.textContent='copy';},1200);}}
function drill(d){if(d.open&&!d.dataset.loaded){d.dataset.loaded='1';
fetch('/api/node/'+encodeURIComponent(d.dataset.host)).then(function(r){return r.json();})
.then(function(j){d.querySelector('.drill-body').textContent=JSON.stringify(j,null,2);})
.catch(function(e){d.querySelector('.drill-body').textContent='fetch failed: '+e;});}}
function opentarget(){try{var h=location.hash&&document.querySelector(location.hash);
if(h&&h.tagName==='DETAILS'){h.open=true;}}catch(e){}}
window.addEventListener('hashchange',opentarget);opentarget();
"#;

// ---------------------------------------------------------------------------
// The CF-7 aggregator page (§7.8): the §7.4 goatnet band — one row per
// CONFIGURED goatnet, present whether or not it can be heard — above the
// per-goatnet grids, each rendered by goatnet_sections, the exact single-mode
// code path. Read-only, rear-hostable, never load-bearing: this page may
// render; it may not decide.
// ---------------------------------------------------------------------------

pub struct NamedView<'a> {
    pub name: &'a str,
    pub view: View<'a>,
    /// This goatnet's own site console, from its aggregator config entry.
    /// `None` hides the link rather than rendering a dead one.
    pub console_url: Option<&'a str>,
}

/// A page anchor from a goatnet name — alnum and dashes only, so the band can
/// deep-link its own page without minting a query param.
fn anchor_id(name: &str) -> String {
    name.chars()
        .map(|c| if c.is_ascii_alphanumeric() { c.to_ascii_lowercase() } else { '-' })
        .collect()
}

/// This goatnet's reachability from THIS console's vantage — the shared
/// classification both the band and the JSON API render, so the page and the
/// machine surface can never disagree about what "unreachable" means.
pub enum Reach {
    /// Nothing ever heard on the subject scope.
    NeverHeard,
    /// A reading exists but is older than the staleness window.
    Stale { age_ms: i64 },
    /// A reading within the staleness window.
    Live { age_ms: i64 },
}

pub fn reach_of(v: &View) -> Reach {
    match v.latest.rollups.get(v.chosen_site) {
        None => Reach::NeverHeard,
        Some(stored) => {
            let age_ms = v.now_ms - stored.received_unix_ms;
            if age_ms > v.stale_after_ms {
                Reach::Stale { age_ms }
            } else {
                Reach::Live { age_ms }
            }
        }
    }
}

/// The §7.4 worst-grounded-grade collapse, shared by the band and the API.
/// "Grounded" is the grammar's own notion, read off the committed §5.3 colour
/// mapping — a grade is grounded iff it colours red/amber/green; no grade
/// name is hard-coded. Collapsing uses the grammar's worst-wins primitive,
/// never a local rule.
pub struct GradeCollapse {
    /// `(grade, colour)` — None when no grounded grade exists.
    pub worst_grounded: Option<(String, String)>,
    /// Worst grade over ALL reported grades (grounded or not); None when no
    /// node was graded at all.
    pub worst_any: Option<String>,
    pub grounded_n: usize,
    pub ungrounded_n: usize,
}

pub fn collapse_grades(d: &ConsistencyDetailV1, g: &Grammar) -> GradeCollapse {
    let grades: Vec<String> = d.nodes.iter().map(|n| n.grade.clone()).collect();
    let grounded: Vec<String> = grades
        .iter()
        .filter(|gr| {
            matches!(g.color(gr, false).ok().as_deref(), Some("green" | "amber" | "red"))
        })
        .cloned()
        .collect();
    let worst_grounded = if grounded.is_empty() {
        None
    } else {
        let worst = g.worst(&grounded).unwrap_or_else(|_| "indeterminate".into());
        let color = g.color(&worst, false).unwrap_or_else(|_| "idle".into());
        Some((worst, color))
    };
    let worst_any = if grades.is_empty() {
        None
    } else {
        Some(g.worst(&grades).unwrap_or_else(|_| "indeterminate".into()))
    };
    GradeCollapse {
        worst_grounded,
        worst_any,
        grounded_n: grounded.len(),
        ungrounded_n: grades.len() - grounded.len(),
    }
}

struct BandCells {
    chip: String,
    nodes: String,
    serial: String,
    oldest: String,
    heard: String,
    note: String,
}

/// The §7.4 band reading for one goatnet: worst grounded grade, n over
/// expected, current serial, oldest held generation's age. Honesty rules
/// (§7.8): a goatnet with no live envelope is `indeterminate` with the age of
/// its last reading (or "never heard") — never green, never absent. No grade
/// is computed here: node grades arrive on the wire, and collapsing them uses
/// the grammar's own worst-wins primitive — the same call every consumer
/// makes, never a local rule.
fn band_cells(v: &View) -> BandCells {
    let repo_serial = v.intent.0.as_ref().and_then(|d| d.get("serial")).and_then(|s| s.as_u64());
    match v.latest.rollups.get(v.chosen_site) {
        None => {
            let expected = v
                .intent
                .0
                .as_ref()
                .and_then(|d| d.get("members"))
                .and_then(|m| m.as_array())
                .map(|a| a.len());
            BandCells {
                chip: chip_titled(
                    "unmetered",
                    "never heard",
                    "grammar grade: indeterminate — nothing ever heard on this scope; never green, never omitted",
                ),
                nodes: match expected {
                    Some(n) => format!("0 of {n}"),
                    None => "0 of ? (intent refused)".to_string(),
                },
                serial: match repo_serial {
                    Some(s) => format!("repo serial {s}"),
                    None => "intent REFUSED".to_string(),
                },
                oldest: "—".to_string(),
                heard: "never".to_string(),
                note: format!(
                    "nothing heard on ops/consistency/health/{} — unreachable, not deployed, or silently denied; never green, never omitted",
                    v.chosen_site
                ),
            }
        }
        Some(stored) => {
            let age = v.now_ms - stored.received_unix_ms;
            let stale = age > v.stale_after_ms;
            let d = detail_of(&stored.env);
            let (nodes, serial, oldest) = match d {
                Some(d) => (
                    format!("{} of {}", d.nodes_reporting, d.nodes_expected),
                    format!("serial {}", d.current_serial),
                    d.nodes
                        .iter()
                        .map(|n| n.intent_age_seconds)
                        .filter(|a| *a >= 0)
                        .max()
                        .map(|s| age_text(s * 1000).trim_end_matches(" ago").to_string())
                        .unwrap_or_else(|| "—".to_string()),
                ),
                None => ("—".to_string(), "—".to_string(), "—".to_string()),
            };
            if stale {
                return BandCells {
                    chip: chip_titled(
                        "idle",
                        &format!("silent — last heard {}", age_text(age)),
                        "grammar grade: indeterminate — the reading is older than the staleness window; shown as history, never as current",
                    ),
                    nodes,
                    serial,
                    oldest,
                    heard: age_text(age),
                    note: "reading older than the staleness window — shown as history, never as current, never as green".to_string(),
                };
            }
            // The shared §7.4 collapse — the same primitive the JSON API
            // serves, so the page and the machine surface cannot disagree.
            let c = match d {
                Some(d) => collapse_grades(d, v.grammar),
                None => GradeCollapse {
                    worst_grounded: None,
                    worst_any: None,
                    grounded_n: 0,
                    ungrounded_n: 0,
                },
            };
            let (chip_html, note) = match (&c.worst_grounded, &c.worst_any) {
                (_, None) => (
                    env_color_chip(&stored.env.overall_color),
                    "no node graded in this reading — absence of a grade is not health".to_string(),
                ),
                (None, Some(worst_any)) => {
                    // "no grounded grade" in plain words: nodes reported but no
                    // reading grounds a verdict, so nothing live is claimed.
                    let (worst_label, worst_title) = grade_human(worst_any);
                    (
                        chip_titled(
                            "idle",
                            "nothing gradable heard",
                            &format!(
                                "no grounded grade — no node produced a reading that grounds a verdict; grammar worst over all reported grades: {worst_any}"
                            ),
                        ),
                        format!(
                            "{} node(s) reported but none could be graded from live evidence — nothing live is claimed; the worst reported state: {worst_label} ({worst_title})",
                            c.ungrounded_n
                        ),
                    )
                }
                (Some((worst, color)), _) => {
                    let note = if c.ungrounded_n > 0 {
                        format!(
                            "+{} node(s) without a gradable live reading, not folded into this state",
                            c.ungrounded_n
                        )
                    } else {
                        String::new()
                    };
                    (grade_chip(color, worst), note)
                }
            };
            BandCells { chip: chip_html, nodes, serial, oldest, heard: age_text(age), note }
        }
    }
}

// ---------------------------------------------------------------------------
// Drift distance (drift-page-human): HOW FAR have the envs
// drifted, stated only where the data grounds it. IntentV1 carries no
// per-component provenance (bump dates live only as fleet-pins.yaml comments,
// which never reach the artifact), so the only grounded per-component dates
// this console holds are the envs' own deploy-history ledgers, heard as
// ConsistencyReportV1 on each goatnet's fabric. Two claims and only two are
// derivable from them, both facts, neither a verdict:
//
//   - "X ahead since <date>": one env's CONFIRMED ledger tip for the
//     component carries the OTHER env's current digest as its prior_ref —
//     that env provably moved OFF the other's version, and the tip's ts says
//     when. This is direction, grounded.
//   - "X's value deployed <date>": a confirmed tip matches that env's own
//     intent digest — the date that value landed, with the honest caveat
//     that a deploy date alone does not order versions (a rollback is also a
//     deploy).
//
// When neither is derivable the line says "versions differ" and why the
// distance is not statable. NEVER a fabricated count — a generation or
// commit distance would need provenance the artifact does not carry (named
// as a follow-up in the introducing PR, not invented here).
// ---------------------------------------------------------------------------

/// One verified env's provenance context: its intent's component→ledger join
/// keys (the fleet-pins `ledger_component` rule: default = the component
/// name, explicit null = no ledger coverage) and its own heard reports.
struct EnvProvenance<'a> {
    name: &'a str,
    ledger_names: std::collections::BTreeMap<String, Option<String>>,
    latest: &'a Latest,
}

fn provenance_of<'a>(name: &'a str, doc: &Value, latest: &'a Latest) -> EnvProvenance<'a> {
    let mut ledger_names = std::collections::BTreeMap::new();
    if let Some(comps) = doc.get("components").and_then(|v| v.as_array()) {
        for c in comps {
            let Some(n) = c.get("name").and_then(|v| v.as_str()) else { continue };
            let ledger = match c.get("ledger_component") {
                None => Some(n.to_string()),
                Some(Value::String(s)) => Some(s.clone()),
                Some(_) => None, // explicit null (or malformed): no ledger coverage
            };
            ledger_names.insert(n.to_string(), ledger);
        }
    }
    EnvProvenance { name, ledger_names, latest }
}

/// The digest of a ref-shaped value, or None.
fn digest_of(s: &str) -> Option<&str> {
    let (_, d) = s.split_once('@')?;
    if is_digest(d) {
        Some(d)
    } else {
        None
    }
}

/// The single digest an env's resolved cells agree on for this row — None
/// when the env has no digest-bearing cell or its own members disagree
/// (distance is then not statable for that env, and nothing is invented).
fn env_value_digest(row: &GradedRow, env: &str) -> Option<String> {
    let prefix = format!("{env}:");
    let mut digests = std::collections::BTreeSet::new();
    for (col, cell) in &row.cells {
        if col != env && !col.starts_with(&prefix) {
            continue;
        }
        if cell.get("state").and_then(|s| s.as_str()) != Some("resolved") {
            continue;
        }
        let v = cell.get("value").and_then(|v| v.as_str())?;
        digests.insert(digest_of(v)?.to_string());
    }
    if digests.len() == 1 {
        digests.into_iter().next()
    } else {
        None
    }
}

/// This env's CONFIRMED deploy-ledger evidence for a component value:
/// `(earliest tip ts whose ref matches `own`, earliest tip ts that moved off
/// `other`)`. Unreconciled tips are excluded — §5.2's ordering caveat makes
/// them optimistic, and a date claim may not rest on an optimistic row.
fn ledger_evidence(
    p: &EnvProvenance,
    component: &str,
    own: &str,
    other: Option<&str>,
) -> (Option<i64>, Option<i64>) {
    let Some(Some(ledger)) = p.ledger_names.get(component) else { return (None, None) };
    let mut deployed: Option<i64> = None;
    let mut moved_off: Option<i64> = None;
    for stored in p.latest.reports.values() {
        for c in &stored.report.components {
            if c.component != *ledger || c.reconciled != "confirmed" || c.ts_unix_ms <= 0 {
                continue;
            }
            if digest_of(&c.r#ref) != Some(own) {
                continue;
            }
            deployed = Some(deployed.map_or(c.ts_unix_ms, |t| t.min(c.ts_unix_ms)));
            if other.is_some() && digest_of(&c.prior_ref) == other {
                moved_off = Some(moved_off.map_or(c.ts_unix_ms, |t| t.min(c.ts_unix_ms)));
            }
        }
    }
    (deployed, moved_off)
}

/// The distance line for one drift-table row, or "" when the row is not the
/// kind distance applies to (only differing component rows carry versions).
fn drift_distance_html(row: &GradedRow, provs: &[EnvProvenance]) -> String {
    if row.subject != "component" || !(row.grade == "SKEW" || row.grade == "declared") {
        return String::new();
    }
    let digests: Vec<(&EnvProvenance, String)> = provs
        .iter()
        .filter_map(|p| env_value_digest(row, p.name).map(|d| (p, d)))
        .collect();
    if digests.len() < 2 {
        return String::new();
    }
    let distinct: std::collections::BTreeSet<&String> =
        digests.iter().map(|(_, d)| d).collect();
    if distinct.len() < 2 {
        return String::new();
    }

    let mut ahead: Vec<String> = Vec::new();
    let mut dated: Vec<String> = Vec::new();
    let mut any_date = false;
    for (i, (p, own)) in digests.iter().enumerate() {
        let (deployed, _) = ledger_evidence(p, &row.name, own, None);
        for (j, (q, theirs)) in digests.iter().enumerate() {
            if i == j || own == theirs {
                continue;
            }
            let (_, off) = ledger_evidence(p, &row.name, own, Some(theirs));
            if let Some(ts) = off {
                ahead.push(format!(
                    "{} ahead since {} — its deploy ledger moved off {}'s current version then",
                    p.name,
                    civil_date(ts),
                    q.name,
                ));
            }
        }
        match deployed {
            Some(ts) => {
                any_date = true;
                dated.push(format!("{} deployed its value {}", p.name, civil_date(ts)));
            }
            None => dated.push(format!("{}'s deploy date unheard", p.name)),
        }
    }
    let sentence = if !ahead.is_empty() {
        format!("versions differ — {}", ahead.join("; "))
    } else if any_date {
        format!(
            "versions differ — {} (deploy-ledger dates; a deploy date alone does not order versions)",
            dated.join(", "),
        )
    } else {
        "versions differ — how far is not statable: no confirmed deploy-ledger record heard for this component".to_string()
    };
    format!(r#"<div class="drift-distance">{}</div>"#, esc(&sentence))
}

// ---------------------------------------------------------------------------
// The cross-env drift panel — the platform-admin question ("is there drift
// between the envs, and does it matter") answered above the fold. The
// comparison is `goat_consistency::diff_intents` over the committed VERIFIED
// artifacts — the library's own grade_row with one column per
// (goatnet, member) — computed at render time and stored nowhere. This
// renderer contributes layout only.
// ---------------------------------------------------------------------------

fn section_cross_env(goatnets: &[NamedView]) -> String {
    let verified: Vec<(&str, &Value)> = goatnets
        .iter()
        .filter_map(|nv| nv.view.intent.0.as_ref().map(|doc| (nv.name, doc)))
        .collect();
    let refused: Vec<&NamedView> =
        goatnets.iter().filter(|nv| nv.view.intent.0.is_none()).collect();
    let mut refused_note = String::new();
    for nv in &refused {
        refused_note.push_str(&format!(
            r#"<p class="refusal-text">{} is NOT compared — its intent refused verification: {}</p>"#,
            esc(nv.name),
            esc(&nv.view.intent.1),
        ));
    }

    if verified.len() < 2 {
        return format!(
            r#"<section class="panel">
<h2>cross-env drift</h2>
<p><strong>not comparable</strong> — a cross-env comparison needs at least two goatnets
with verified committed intent; {} of {} configured goatnet(s) verified. That is an
answer, not a blank: nothing below is a drift verdict.</p>
{}
</section>"#,
            verified.len(),
            goatnets.len(),
            refused_note,
        );
    }

    let grammar = goatnets[0].view.grammar;
    let envs: Vec<EnvIntent> =
        verified.iter().map(|(name, doc)| EnvIntent { env: name, intent: doc }).collect();
    let today = goat_consistency::utc_date(goatnets[0].view.now_ms);
    let report = match diff_intents(&envs, grammar, Some(&today)) {
        Ok(r) => r,
        Err(e) => {
            return format!(
                r#"<section class="panel refusal"><h2>cross-env drift — REFUSED</h2>
<p class="refusal-text">the grading library refused: {}</p></section>"#,
                esc(&e.to_string())
            )
        }
    };

    // ONE coherent summary (drift-page-human): every row counted
    // in exactly ONE category, the denominator stated once up front, admin
    // words in the sentence — the grammar term stays on the row chips and the
    // API, and each summary chip carries its grammar grade on hover. The
    // drift signal leads (a zero is stated, never omitted); the rest follow
    // in the grammar's worst-first precedence, matches last.
    let total = report.rows.len();
    let undeclared = report.undeclared();
    let names: Vec<&str> = verified.iter().map(|(n, _)| *n).collect();
    let mut summary_chips: Vec<String> = Vec::new();
    if undeclared > 0 {
        summary_chips.push(format!(
            r#"<span class="chip chip-amber drift-chip" title="grammar grade: SKEW — a difference nothing declares; THE drift signal">{undeclared} undeclared drift</span>"#
        ));
    } else {
        summary_chips.push(
            r#"<span class="chip chip-green drift-chip" title="grammar grade: SKEW — zero rows carry an undeclared difference">no undeclared drift</span>"#
                .to_string(),
        );
    }
    for (grade, title) in [
        ("unreadable", "grammar grade: unreadable — a value is present but could not be evaluated; never a match"),
        ("indeterminate", "grammar grade: indeterminate — governed in one env's artifact only, or nothing to read"),
        ("unaccountable", "grammar grade: unaccountable — a reported value no generation in the tree explains"),
        ("declared", "grammar grade: declared — a difference somebody declared with a reason; a correct state"),
        ("match", "grammar grade: match — every compared value agrees"),
    ] {
        let Some(&n) = report.by_grade.get(grade) else { continue };
        let color = grammar.color(grade, false).unwrap_or_else(|_| "idle".into());
        let s = if n == 1 { "" } else { "s" };
        let text = match grade {
            "declared" => format!("{n} declared difference{s}"),
            "match" => format!("{n} match{}", if n == 1 { "" } else { "es" }),
            "unreadable" => format!("{n} unreadable value{s}"),
            "indeterminate" => format!("{n} not comparable"),
            "unaccountable" => format!("{n} unexplained value{s}"),
            _ => format!("{n} {grade}"),
        };
        summary_chips.push(chip_titled(&color, &text, title));
    }
    let summary = format!(
        r#"{total} rows compared across {}: {}"#,
        esc(&names.join(" + ")),
        summary_chips.join(" &middot; "),
    );

    // WHICH generation each verdict was computed FOR, and how old it is.
    // The console serves the intent artifacts baked into its image at deploy
    // time, so a page rendering a stale generation is indistinguishable from a
    // current one unless it says so.
    // a day is flagged, because that is the age at which "the tree says X" has
    // most likely stopped being true. Age needs no host: it is this process's
    // clock against a stamp inside the artifact.
    let now_ms = goatnets[0].view.now_ms;
    let mut gen_bits: Vec<String> = Vec::new();
    for (name, doc) in &verified {
        let serial = doc.get("serial").and_then(|v| v.as_i64());
        let gen_at = doc.get("generated_at").and_then(|v| v.as_str());
        let age_ms = gen_at.and_then(goat_consistency::parse_utc_stamp).map(|t| now_ms - t * 1000);
        let (age, stale) = match age_ms {
            Some(ms) => (age_text(ms), ms >= 86_400_000),
            None => ("age unstatable".to_string(), false),
        };
        gen_bits.push(format!(
            r#"<span class="{}" title="{}">{} serial {} &middot; {}</span>"#,
            if stale { "chip chip-amber gen-stale" } else { "gen-fresh" },
            esc(&format!(
                "generated {} — this page compares the artifact generation it holds, not the tip of the tree",
                gen_at.unwrap_or("(no stamp)")
            )),
            esc(name),
            serial.map(|s| s.to_string()).unwrap_or_else(|| "?".into()),
            esc(&age),
        ));
    }
    let generations = format!(
        r#"<p class="drift-generations">computed for {} &middot; dated {}</p>"#,
        gen_bits.join(" &middot; "),
        esc(&today),
    );

    // Grounded distance needs each env's ledger reports beside its artifact.
    let provs: Vec<EnvProvenance> = goatnets
        .iter()
        .filter_map(|nv| {
            nv.view.intent.0.as_ref().map(|doc| provenance_of(nv.name, doc, nv.view.latest))
        })
        .collect();

    // The diff table: one column per (goatnet, member) the rows carry.
    // Env-level synthetic cells (key `<env>`, no member) are not columns —
    // they fold into their env's member columns in value_cells_html.
    let mut columns: Vec<String> = Vec::new();
    for row in &report.rows {
        for c in &row.columns {
            if c.contains(':') && !columns.contains(c) {
                columns.push(c.clone());
            }
        }
    }
    columns.sort();
    let head = ghead_html(&columns, true);
    // Row order = the panel's own priority statement, applied: undeclared SKEW
    // first (THE drift signal), then the remaining grades in the grammar's
    // worst-first precedence, matches (compact, self-evident) last. A stable
    // sort keeps artifact order within a grade. Presentation order only — the
    // verdicts, counts and API order are untouched.
    let mut ordered: Vec<&GradedRow> = report.rows.iter().collect();
    ordered.sort_by_key(|r| (r.grade != "SKEW", grammar.rank(&r.grade).unwrap_or(usize::MAX)));
    // Section flow inside the table (drift-page-polish): problems → declared →
    // matches, each group bounded by its own heading row so the eye lands on
    // what needs attention and can stop there. Grouping is presentation over
    // the same sorted rows — verdicts, counts and API order untouched.
    let group_of = |grade: &str| match grade {
        "match" => 2usize,
        "declared" => 1,
        _ => 0,
    };
    let group_names = ["needs attention", "declared differences", "matches"];
    // Row-level declared reasons from every compared artifact (first found
    // wins) — so a capability row declared in the capabilities shape prints
    // the declarer's words, same as a component row.
    let mut reasons = std::collections::BTreeMap::new();
    for (_, doc) in &verified {
        for (k, r) in row_reasons(doc) {
            reasons.entry(k).or_insert(r);
        }
    }
    let mut body = String::new();
    let mut current_group: Option<usize> = None;
    for row in ordered {
        let g = group_of(&row.grade);
        if current_group != Some(g) {
            current_group = Some(g);
            let n = report.rows.iter().filter(|r| group_of(&r.grade) == g).count();
            body.push_str(&format!(r#"<div class="ggroup">{} ({n})</div>"#, group_names[g]));
        }
        let color = grammar.color(&row.grade, false).unwrap_or_else(|_| "idle".into());
        let distance = drift_distance_html(row, &provs);
        let fallback = reasons.get(&(row.subject.clone(), row.name.clone()));
        body.push_str(&grid_row_html_extra(
            row,
            &color,
            &columns,
            &distance,
            fallback.map(String::as_str),
        ));
    }

    format!(
        r#"<section class="panel">
<h2>cross-env drift <span class="dim">(committed intent vs committed intent — no host consulted)</span></h2>
<p class="drift-summary">{}</p>
{}
{}
<div class="grid" style="--envn:{envn}">{}{}</div>
<p class="grounding">the comparison is the shared grading library's own grade_row over one column per
(goatnet, member) — cells copied verbatim from each goatnet's verified committed IntentV1, computed at
render time, stored nowhere. The page speaks plain words; the grammar's own term for every verdict sits
on its chip's hover, and the API serves the grammar untouched. A row's distance line states only what an
env's deploy ledger grounds — dates, and direction only when a ledger provably moved off the other env's
version. node_shape is not compared (ADR 1084 — nodes are deliberately unequal); retired/ungoverned are
per-goatnet assertions and are not compared. Counts with denominators, no percentages. A declared
deviation carrying an `until:` stops covering its row once that date has passed — the row falls back to
the verdict its values earn and says which declaration lapsed.</p>
</section>"#,
        summary,
        generations,
        refused_note,
        head,
        body,
        envn = columns.len(),
    )
}

pub fn render_aggregate_page(goatnets: &[NamedView], version: &str) -> String {
    let mut band_rows = String::new();
    let mut sections = String::new();
    for nv in goatnets {
        let id = anchor_id(nv.name);
        let b = band_cells(&nv.view);
        band_rows.push_str(&format!(
            r##"<tr><td class="mono"><a class="gn-link" href="#gn-{id}">{}</a></td><td>{}</td><td class="mono">{}</td><td class="mono">{}</td><td class="mono">{}</td><td class="mono">{}</td><td class="band-note dim">{}</td></tr>"##,
            esc(nv.name),
            b.chip,
            b.nodes,
            b.serial,
            b.oldest,
            b.heard,
            esc(&b.note),
        ));
        // The full grid stack moves BEHIND a drill-down: the landing view is
        // the drift panel + the band; each goatnet's complete single-mode
        // stack is one click (or one band-row link) away, rendered in full
        // either way — collapsed is presentation, never omission.
        sections.push_str(&format!(
            r#"<details class="goatnet" id="gn-{id}">
<summary class="gn-head">full grid — goatnet <span class="mono">{}</span> <span class="dim">(subject scope <span class="mono">{}</span>)</span>{}</summary>
{}
</details>"#,
            esc(nv.name),
            esc(nv.view.chosen_site),
            console_link(nv.console_url),
            goatnet_sections(&nv.view),
        ));
    }
    let cross_env = section_cross_env(goatnets);

    format!(
        r#"<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="30">
<title>consistency — cross-goatnet drift</title>
<style>{}</style>
</head>
<body>
<header>
<h1>consistency <span class="mono">cross-goatnet drift</span></h1>
<p class="tagline">is there drift between the envs, and does it matter?
&mdash; a read-only projection of {} goatnet(s); this page may render, it may not decide,
and killing it changes nothing on any goatnet. <a href="/release">release readiness &rarr;</a></p>
</header>
{}
<section class="panel">
<h2>goatnet band <span class="dim">(each env's own live convergence)</span></h2>
<table class="rows band">
<thead><tr><th>goatnet</th><th title="the grammar's worst-wins collapse over this goatnet's grounded node grades">live state</th><th>nodes reporting</th><th>generation</th><th>oldest held generation</th><th>last heard</th><th>note</th></tr></thead>
<tbody>{}</tbody>
</table>
<p class="grounding">one row per <em>configured</em> goatnet — an unreachable one says so with the age of its
last reading, never green, never absent. Grades arrive on each goatnet's own wire;
this page collapses them only with the grammar's worst-wins rule and computes nothing else.</p>
</section>
{}
<footer>
<p class="dim">goat-consistency-console v{} in aggregator mode &middot; ADR 1110 D5 / consistency-foundation.md §7.8 &middot;
read-only, never load-bearing: no node, deploy, rollback or health publication reads this page.
Each goatnet's grid is the same code path as its own single-mode console.
No percentages by design: counts with denominators, or nothing.</p>
</footer>
<script>{}</script>
</body>
</html>"#,
        STYLE,
        goatnets.len(),
        cross_env,
        band_rows,
        sections,
        esc(version),
        PAGE_SCRIPT,
    )
}
