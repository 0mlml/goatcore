// SPDX-License-Identifier: Apache-2.0
//
// grammar.rs — the Rust loader for ops/consistency/grammar.yaml.
//
// Like its Python sibling (ops/consistency/python/goat_consistency/grammar.py)
// this module READS the grammar; it does not contain one. Every grade name,
// every precedence position, every remediation pointer and every colour comes
// out of the committed YAML, because the whole point of ADR 1110 D2 is that the
// grammar is DATA that two implementations execute rather than code that one of
// them owns. If you find yourself about to hard-code a grade name here, the
// corpus is where that behaviour belongs.
//
// THE GRAMMAR IS EMBEDDED AT COMPILE TIME, not read from disk. The Python side
// reads the file because it runs from a checkout; this side ships in a
// container that has no checkout, and a runtime read would introduce a
// "grammar file missing" degraded mode for a file that is not optional. Baking
// it in means a deployed binary provably executes the committed grammar it was
// built from, and a grammar edit is a rebuild — which is correct, because a
// grammar edit IS a behaviour change.
//
// Canon: docs/design/consistency-foundation.md §4.2.

use std::collections::BTreeMap;
use std::fmt;

/// The committed grammar, compiled in. `../../../grammar.yaml` resolves to
/// ops/consistency/grammar.yaml — the one the Python implementation and the
/// golden corpus both read.
pub const GRAMMAR_YAML: &str = include_str!("../../../grammar.yaml");

#[derive(Debug)]
pub struct GrammarError(pub String);

impl fmt::Display for GrammarError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.0)
    }
}

impl std::error::Error for GrammarError {}

macro_rules! bail {
    ($($arg:tt)*) => { return Err(GrammarError(format!($($arg)*))) };
}

#[derive(Debug, Clone, serde::Deserialize)]
struct GradeSpec {
    name: String,
    color: Option<String>,
    color_when_scope_empty: Option<String>,
    needs_remediation: Option<bool>,
    #[allow(dead_code)]
    badge: Option<String>,
}

#[derive(Debug, Clone, serde::Deserialize)]
struct KindSpec {
    pointer: String,
    #[serde(default)]
    refuses: bool,
}

/// A dispatch target is either a kind name or a by-artifact-type table. The
/// second form exists because `unaccountable` dispatches on the ADR 1060
/// artifact type — rollback of a `data` or `cert` artifact is a lie, and the
/// grammar says so rather than a renderer discovering it.
#[derive(Debug, Clone, serde::Deserialize)]
#[serde(untagged)]
enum Dispatch {
    Kind(String),
    ByType {
        by_artifact_type: BTreeMap<String, String>,
    },
}

#[derive(Debug, Clone, serde::Deserialize)]
struct RemediationSpec {
    #[serde(default)]
    kinds: BTreeMap<String, KindSpec>,
    #[serde(default)]
    dispatch: BTreeMap<String, BTreeMap<String, Dispatch>>,
}

/// One coverage state (ADR 1125): whether a comparison is POSSIBLE, which is
/// the question that precedes "do the values agree". Its remediation comes
/// from the same closed set the grades dispatch into — `null` for `observed`,
/// because there is nothing to fix.
#[derive(Debug, Clone, serde::Deserialize)]
struct CoverageSpec {
    name: String,
    #[allow(dead_code)]
    meaning: Option<String>,
    #[serde(default)]
    remediation: Option<String>,
}

#[derive(Debug, Clone, serde::Deserialize)]
struct CoverageBlock {
    #[serde(default)]
    states: Vec<CoverageSpec>,
}

#[derive(Debug, Clone, serde::Deserialize)]
struct RawGrammar {
    version: Option<u32>,
    #[serde(default)]
    grades: Vec<GradeSpec>,
    #[serde(default)]
    precedence: Vec<String>,
    #[serde(default)]
    remediation: Option<RemediationSpec>,
    #[serde(default)]
    coverage: Option<CoverageBlock>,
}

/// One remediation pointer, resolved. Emitted as DATA on a graded row (§6.3):
/// every surface shows the SAME command for the same condition, because none of
/// them writes the sentence.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize)]
pub struct Remediation {
    pub kind: String,
    pub pointer: String,
    pub refuses: bool,
}

#[derive(Debug, Clone)]
pub struct Grammar {
    pub version: u32,
    grades: BTreeMap<String, GradeSpec>,
    precedence: Vec<String>,
    kinds: BTreeMap<String, KindSpec>,
    dispatch: BTreeMap<String, BTreeMap<String, Dispatch>>,
    /// coverage state -> remediation kind (ADR 1125). Absent value = the state
    /// declares no remediation, which only `observed` does.
    coverage: BTreeMap<String, Option<String>>,
}

impl Grammar {
    /// The compiled-in grammar. Panics only if the committed YAML is not a
    /// usable grammar, which is not a degraded mode: every grade in the system
    /// is defined by that file.
    pub fn load() -> Result<Self, GrammarError> {
        Self::parse(GRAMMAR_YAML)
    }

    pub fn parse(text: &str) -> Result<Self, GrammarError> {
        let raw: RawGrammar = serde_yaml::from_str(text)
            .map_err(|e| GrammarError(format!("ops/consistency/grammar.yaml is not parseable: {e}")))?;
        let grades: BTreeMap<String, GradeSpec> =
            raw.grades.into_iter().map(|g| (g.name.clone(), g)).collect();
        let rem = raw.remediation.unwrap_or(RemediationSpec {
            kinds: BTreeMap::new(),
            dispatch: BTreeMap::new(),
        });
        let coverage: BTreeMap<String, Option<String>> = raw
            .coverage
            .map(|c| c.states.into_iter().map(|s| (s.name, s.remediation)).collect())
            .unwrap_or_default();
        let g = Grammar {
            version: raw.version.unwrap_or(0),
            grades,
            precedence: raw.precedence,
            kinds: rem.kinds,
            dispatch: rem.dispatch,
            coverage,
        };
        g.validate()?;
        Ok(g)
    }

    /// The same validation the Python loader performs, for the same reason: a
    /// malformed grammar must fail loud rather than grade quietly.
    fn validate(&self) -> Result<(), GrammarError> {
        if self.grades.is_empty() {
            bail!("no grades declared");
        }
        let declared: std::collections::BTreeSet<&String> = self.grades.keys().collect();
        let ordered: std::collections::BTreeSet<&String> = self.precedence.iter().collect();
        if declared != ordered {
            let missing: Vec<&&String> = declared.difference(&ordered).collect();
            let extra: Vec<&&String> = ordered.difference(&declared).collect();
            bail!(
                "precedence and grades disagree (missing from precedence: {missing:?}; \
                 unknown in precedence: {extra:?}) — a grade with no precedence position \
                 cannot be compared, which is worse than not having it"
            );
        }
        for (name, spec) in &self.grades {
            if spec.needs_remediation.is_none() {
                bail!("grade '{name}' does not say whether it needs a remediation pointer");
            }
            if spec.color.as_deref().unwrap_or("").is_empty() {
                bail!("grade '{name}' has no colour; a grade that cannot render is not a grade");
            }
        }
        for (state, kind) in &self.coverage {
            if let Some(k) = kind {
                if !self.kinds.contains_key(k) {
                    bail!("coverage state '{state}' names unknown remediation kind '{k}'");
                }
            }
        }
        for (vantage, table) in &self.dispatch {
            for (grade, target) in table {
                if !self.grades.contains_key(grade) {
                    bail!("dispatch[{vantage}] maps unknown grade '{grade}'");
                }
                for kind in Self::targets(target) {
                    if !self.kinds.contains_key(kind) {
                        bail!("dispatch[{vantage}][{grade}] names unknown remediation kind '{kind}'");
                    }
                }
            }
        }
        Ok(())
    }

    fn targets(target: &Dispatch) -> Vec<&String> {
        match target {
            Dispatch::Kind(k) => vec![k],
            Dispatch::ByType { by_artifact_type } => by_artifact_type.values().collect(),
        }
    }

    pub fn grade_names(&self) -> Vec<&str> {
        self.grades.keys().map(|s| s.as_str()).collect()
    }

    /// Every declared coverage state, for a surface that wants to render the
    /// full set (including the ones with a count of zero — a category omitted
    /// because it is empty reads as a category that does not exist).
    pub fn coverage_states(&self) -> Vec<&str> {
        self.coverage.keys().map(|s| s.as_str()).collect()
    }

    /// The remediation kind a coverage state dispatches to, or `None` when the
    /// state declares none (`observed`). An UNKNOWN state is an error, not a
    /// `None`: a surface inventing a coverage state must fail loud, the same
    /// posture `color()` takes for an unknown grade.
    pub fn coverage_remediation(&self, state: &str) -> Result<Option<&str>, GrammarError> {
        match self.coverage.get(state) {
            Some(k) => Ok(k.as_deref()),
            None => Err(GrammarError(format!(
                "unknown coverage state '{state}' — ops/consistency/grammar.yaml declares {:?}",
                self.coverage_states()
            ))),
        }
    }

    // -- the comparison primitives -------------------------------------------

    pub fn rank(&self, grade: &str) -> Result<usize, GrammarError> {
        self.precedence
            .iter()
            .position(|g| g == grade)
            .ok_or_else(|| GrammarError(format!("'{grade}' is not a grade in this grammar")))
    }

    /// Worst-wins, the rule for collapsing cells into a row and rows into a
    /// scope. Empty input is `indeterminate`: a scope with nothing in it has not
    /// been observed, and absence of signal is never green.
    pub fn worst<S: AsRef<str>>(&self, grades: &[S]) -> Result<String, GrammarError> {
        if grades.is_empty() {
            return Ok("indeterminate".to_string());
        }
        let mut best: Option<(usize, &str)> = None;
        for g in grades {
            let g = g.as_ref();
            let r = self.rank(g)?;
            if best.map(|(br, _)| r < br).unwrap_or(true) {
                best = Some((r, g));
            }
        }
        Ok(best.expect("non-empty").1.to_string())
    }

    pub fn color(&self, grade: &str, scope_empty: bool) -> Result<String, GrammarError> {
        let spec = self
            .grades
            .get(grade)
            .ok_or_else(|| GrammarError(format!("'{grade}' is not a grade in this grammar")))?;
        if scope_empty {
            if let Some(c) = &spec.color_when_scope_empty {
                return Ok(c.clone());
            }
        }
        Ok(spec.color.clone().unwrap_or_default())
    }

    pub fn needs_remediation(&self, grade: &str) -> Result<bool, GrammarError> {
        let spec = self
            .grades
            .get(grade)
            .ok_or_else(|| GrammarError(format!("'{grade}' is not a grade in this grammar")))?;
        Ok(spec.needs_remediation.unwrap_or(false))
    }

    /// Which kind of pointer this grade earns from this vantage, or None when
    /// the grade needs no pointer.
    pub fn remediation_kind(
        &self,
        grade: &str,
        vantage: &str,
        artifact_type: Option<&str>,
    ) -> Result<Option<String>, GrammarError> {
        if !self.needs_remediation(grade)? {
            return Ok(None);
        }
        let Some(target) = self.dispatch.get(vantage).and_then(|t| t.get(grade)) else {
            return Ok(None);
        };
        Ok(match target {
            Dispatch::Kind(k) => Some(k.clone()),
            Dispatch::ByType { by_artifact_type } => {
                artifact_type.and_then(|t| by_artifact_type.get(t).cloned())
            }
        })
    }

    /// Fill a pointer template. `{}` placeholders are replaced from the row's
    /// own scope; the result is printed VERBATIM by every renderer.
    pub fn pointer(
        &self,
        kind: &str,
        fields: &[(&str, &str)],
    ) -> Result<Remediation, GrammarError> {
        let spec = self
            .kinds
            .get(kind)
            .ok_or_else(|| GrammarError(format!("'{kind}' is not a remediation kind in this grammar")))?;
        let mut pointer = spec.pointer.clone();
        for (key, value) in fields {
            pointer = pointer.replace(&format!("{{{key}}}"), value);
        }
        Ok(Remediation {
            kind: kind.to_string(),
            pointer,
            refuses: spec.refuses,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_committed_grammar_loads_and_validates() {
        let g = Grammar::load().expect("the committed grammar must be a usable grammar");
        assert!(g.version >= 1);
        // Every grade has a colour and a remediation rule — the invariant the
        // Python runner prints on success.
        for name in g.grade_names() {
            assert!(!g.color(name, false).unwrap().is_empty(), "{name} has no colour");
            g.needs_remediation(name).unwrap();
        }
    }

    #[test]
    fn worst_wins_and_empty_is_never_green() {
        let g = Grammar::load().unwrap();
        assert_eq!(g.worst(&["match", "SKEW"]).unwrap(), "SKEW");
        assert_eq!(g.worst(&["SKEW", "unreadable"]).unwrap(), "unreadable");
        assert_eq!(g.worst::<&str>(&[]).unwrap(), "indeterminate");
    }

    #[test]
    fn a_malformed_grammar_fails_loud_rather_than_grading_quietly() {
        // A grade with no precedence position cannot be compared.
        let bad = "version: 1\ngrades:\n  - name: match\n    color: green\n    needs_remediation: false\nprecedence: []\n";
        assert!(Grammar::parse(bad).is_err());
    }
}
