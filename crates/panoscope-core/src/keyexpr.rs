// SPDX-License-Identifier: Apache-2.0
//
// Key-expression helpers. Coarse lexical heuristic for v1; refined when
// Block 23 lineage lands.

/// Two key expressions are "lexically related" if either is a prefix of
/// the other after stripping the trailing wildcard segments (`/*`, `/**`).
pub fn keyexprs_related(a: &str, b: &str) -> bool {
    let aa = strip_wildcards(a);
    let bb = strip_wildcards(b);
    aa.starts_with(&bb) || bb.starts_with(&aa)
}

pub fn strip_wildcards(k: &str) -> String {
    k.trim_end_matches("/**")
        .trim_end_matches("/*")
        .to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keyexprs_related_handles_wildcards() {
        assert!(keyexprs_related(
            "org11x/lattice/**",
            "org11x/lattice/entity/track"
        ));
        assert!(keyexprs_related(
            "org11x/lattice/entity/track",
            "org11x/lattice/**"
        ));
        assert!(!keyexprs_related("org11x/**", "org116x/**"));
        assert!(keyexprs_related(
            "org11x/lattice/entity",
            "org11x/lattice/entity/track/v1"
        ));
    }

    #[test]
    fn strip_wildcards_removes_trailing_stars() {
        assert_eq!(strip_wildcards("a/b/**"), "a/b");
        assert_eq!(strip_wildcards("a/b/*"), "a/b");
        assert_eq!(strip_wildcards("a/b"), "a/b");
        assert_eq!(strip_wildcards(""), "");
    }
}
