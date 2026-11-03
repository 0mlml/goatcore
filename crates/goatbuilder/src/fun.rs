//! Hidden easter eggs, held to goat's Three Rules of delight
//! (docs/design/goat-cli-onboarding-and-delight.md), applied here strictly:
//!   1. Never on the machine surface — nothing here touches `-o json|yaml`,
//!      exit codes of real verbs, records, completions, or a parity-gated line.
//!   2. Opt-in only — every egg is a hidden verb that runs only when typed.
//!      Nothing appears unprompted.
//!   3. Nothing core is a joke — no egg uses, names or parodies how
//!      goatbuilder decides or proves anything. The material is goats, and
//!      build-tool lore that is plainly a joke.

/// `goatbuilder make` — make(1)'s famous shrug, with a fact about goats:
/// a goat is not made. A goat is kidded (the word for a goat giving birth).
pub const MAKE: &str = "goatbuilder: Nothing to be done for 'goat'. Goats are not made; they are kidded.";

/// `goatbuilder make me a sandwich` — the old terminal exchange. Asked
/// plainly, it declines; asked insistently (`--yes`, standing in for `sudo`),
/// it agrees, and a goat does what goats do with a sandwich.
pub const SANDWICH: &str = "What? Make it yourself.";
pub const SANDWICH_INSIST: &str = "Okay. (The goat ate it.)";

/// `goatbuilder argan` — the goats of the argan forest, which climb the
/// trees for the fruit and stand in the branches as if they had always
/// belonged there.
pub const ARGAN: &str = r#"
                 _ ,  _
          ___  (o o) ( )___     __
       __(   )_ \V/ _(  o o)___(  )__
      (_  (o o)  |  (__ \V/  _   ___ )
        (__\V/__ | __/  |   (o o)    )
           \___\_|_/___/     \V/ ___/
                 | |
                 | |      goats climb argan trees for the fruit.
            _____|_|_____   nobody told them they couldn't.
"#;

pub fn make(args: &[String], insist: bool) -> i32 {
    let words: Vec<String> = args.iter().map(|a| a.to_lowercase()).collect();
    if words == ["me", "a", "sandwich"] {
        println!("{}", if insist { SANDWICH_INSIST } else { SANDWICH });
    } else {
        println!("{MAKE}");
    }
    0
}

pub fn argan() -> i32 {
    print!("{ARGAN}");
    0
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn make_shrugs_and_knows_its_goats() {
        assert_eq!(make(&[], false), 0);
        assert!(MAKE.contains("Nothing to be done for 'goat'"));
        assert!(MAKE.contains("kidded"));
    }

    #[test]
    fn sandwich_declines_then_agrees_when_insisted() {
        let w: Vec<String> = ["Me", "a", "SANDWICH"].iter().map(|s| s.to_string()).collect();
        assert_eq!(make(&w, false), 0);
        assert_eq!(make(&w, true), 0);
        assert_eq!(SANDWICH, "What? Make it yourself.");
        assert!(SANDWICH_INSIST.starts_with("Okay."));
    }

    #[test]
    fn argan_answers_and_exits_zero() {
        assert_eq!(argan(), 0);
        assert!(ARGAN.contains("argan trees"));
    }

    #[test]
    fn the_eggs_are_hidden_from_help_and_completions() {
        use clap::CommandFactory;
        let cmd = crate::Cli::command();
        for name in ["make", "argan"] {
            let sub = cmd.find_subcommand(name).unwrap_or_else(|| panic!("`{name}` is wired"));
            assert!(sub.is_hide_set(), "`{name}` must be hidden");
        }
    }

    #[test]
    fn no_egg_touches_a_core_concept_or_reads_like_a_real_row() {
        for s in [MAKE, SANDWICH, SANDWICH_INSIST, ARGAN] {
            let l = s.to_lowercase();
            for core in ["ledger", "phoenix", "rebuild", "declar", "converg", "verif", "digest", "release", "soak", "rollback", "deploy", "idempot", "one-hour", "refused", "fail", "gb-"] {
                assert!(!l.contains(core), "egg mentions `{core}`: {s}");
            }
            assert!(!s.starts_with("ok "), "{s}");
        }
    }
}
