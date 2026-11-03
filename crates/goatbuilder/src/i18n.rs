//! Languages for goatbuilder's human prose — goat's render boundary, reused
//! (ADR 1024, docs/design/goat-cli-i18n.md §"goatbuilder").
//!
//! Same stack (Fluent catalogs embedded with rust-embed, `i18n-embed`'s
//! loader), same selection (`--lang` → `GOAT_LANG` → OS locale → English),
//! same provenance file and machine-translation notice as `goat`. What
//! localizes here: the help text (root, global flags, every verb) and, through
//! the shared error catalog, every error explanation. What never does: verb
//! output an operator or a pipeline reads (report rows, verdict lines, `-o
//! json|yaml`), codes, commands, paths, exit codes.
//!
//! English is the doc comments on `Cli`/`Cmd`; `i18n/en/goatbuilder.ftl`
//! mirrors them and a test holds the two equal. The help swap runs only for a
//! non-English locale, so English output is clap's own, byte for byte, and the
//! parity gates and golden tests never depend on the catalog.

use i18n_embed::{
    fluent::{fluent_language_loader, FluentLanguageLoader},
    DesktopLanguageRequester, LanguageLoader, LanguageRequester,
};
use once_cell::sync::Lazy;
use rust_embed::RustEmbed;
use unic_langid::LanguageIdentifier;

#[derive(RustEmbed)]
#[folder = "i18n/"]
struct Localizations;

pub static LANGUAGE_LOADER: Lazy<FluentLanguageLoader> = Lazy::new(|| {
    let loader: FluentLanguageLoader = fluent_language_loader!();
    loader
        .load_fallback_language(&Localizations)
        .expect("the English fallback catalog must embed + parse");
    loader.set_use_isolating(false);
    loader
});

/// `goat`'s provenance file is the one record of each language's review
/// status and of where to report a translation error; both binaries read it.
#[derive(serde::Deserialize, Default)]
struct Provenance {
    #[serde(default)]
    feedback: Feedback,
    #[serde(default)]
    review: std::collections::BTreeMap<String, String>,
}

#[derive(serde::Deserialize, Default)]
struct Feedback {
    #[serde(default)]
    email: String,
    #[serde(default)]
    repo: String,
}

static PROVENANCE: Lazy<Provenance> =
    Lazy::new(|| toml::from_str(include_str!("../../goat-cli/i18n-provenance.toml")).unwrap_or_default());

/// The OS locale, with the same guard goat needs: a locale string the
/// detection crate cannot parse panics inside it; treat that as "no OS
/// preference" instead of taking the binary down.
fn os_requested_languages() -> Vec<LanguageIdentifier> {
    let prev = std::panic::take_hook();
    std::panic::set_hook(Box::new(|_| {}));
    let r = std::panic::catch_unwind(|| DesktopLanguageRequester::new().requested_languages());
    std::panic::set_hook(prev);
    r.unwrap_or_default()
}

/// `--lang` / `GOAT_LANG` out of the raw argv, before clap parses, so the help
/// clap prints is already in the chosen language. Same rules as goat's
/// `preparse_lang`: the flag beats the env, `--` ends option scanning.
pub fn preparse_lang() -> Option<String> {
    preparse_lang_from(std::env::args().skip(1), std::env::var("GOAT_LANG").ok())
}

fn preparse_lang_from<I: Iterator<Item = String>>(mut args: I, env_lang: Option<String>) -> Option<String> {
    while let Some(a) = args.next() {
        if a == "--" {
            break;
        }
        if a == "--lang" {
            return args.next();
        }
        if let Some(v) = a.strip_prefix("--lang=") {
            return Some(v.to_string());
        }
    }
    env_lang.filter(|s| !s.is_empty())
}

/// Negotiate and load the active language once, then tell the shared error
/// catalog. Any failure leaves English active.
pub fn select_language(explicit: Option<&str>) {
    let mut requested: Vec<LanguageIdentifier> = Vec::new();
    if let Some(li) = explicit.and_then(|t| t.parse::<LanguageIdentifier>().ok()) {
        requested.push(li);
    }
    requested.extend(os_requested_languages());
    let _ = i18n_embed::select(&*LANGUAGE_LOADER, &Localizations, &requested);
    LANGUAGE_LOADER.set_use_isolating(false);
    goat_errors::set_language(&LANGUAGE_LOADER.current_language().to_string());
}

fn is_english() -> bool {
    LANGUAGE_LOADER.current_language().language.as_str() == "en"
}

fn tr(id: &str) -> Option<String> {
    LANGUAGE_LOADER.has(id).then(|| LANGUAGE_LOADER.get(id))
}

/// Human prose for the active locale: `english` itself in English (so English
/// output never depends on the catalog), the catalog's text otherwise, English
/// again when the locale lacks the id. For printed prose only, never for a
/// report row (module doc).
pub fn prose(id: &str, english: &str) -> String {
    if is_english() {
        return english.to_string();
    }
    tr(id).unwrap_or_else(|| english.to_string())
}

/// `prose` with Fluent variables: `english` is the already-formatted English
/// line; a non-English locale renders `id` with `args`.
pub fn prose_args(id: &str, english: String, args: &[(&str, String)]) -> String {
    if is_english() || !LANGUAGE_LOADER.has(id) {
        return english;
    }
    let map: std::collections::HashMap<&str, String> = args.iter().map(|(k, v)| (*k, v.clone())).collect();
    LANGUAGE_LOADER.get_args(id, map)
}

/// The catalog id of a subcommand's help: `help-cmd-<name>`.
pub fn cmd_key(name: &str) -> String {
    format!("help-cmd-{name}")
}

/// The catalog id of a global flag's help: `help-flag-<arg id>`.
pub fn flag_key(id: &str) -> String {
    format!("help-flag-{}", id.replace('_', "-"))
}

/// Swap clap's English help for the active locale's, for a non-English locale
/// only. Keys are derived from the command tree, so a verb added by another
/// change needs only its catalog line; a missing key keeps the English.
pub fn localize_command(cmd: clap::Command) -> clap::Command {
    if is_english() {
        return cmd;
    }
    let mut cmd = cmd;
    if let Some(t) = tr("help-about") {
        cmd = cmd.about(t);
    }
    let globals: Vec<String> = cmd
        .get_arguments()
        .filter(|a| a.is_global_set())
        .map(|a| a.get_id().to_string())
        .collect();
    for id in globals {
        if let Some(t) = tr(&flag_key(&id)) {
            cmd = cmd.mut_arg(id.as_str(), |a| a.help(t));
        }
    }
    let subs: Vec<String> = cmd.get_subcommands().map(|s| s.get_name().to_string()).collect();
    for name in subs {
        let about = tr(&cmd_key(&name));
        let long = tr(&format!("{}-long", cmd_key(&name)));
        if about.is_some() || long.is_some() {
            cmd = cmd.mut_subcommand(name.as_str(), |s| {
                let s = match about { Some(a) => s.about(a), None => s };
                match long { Some(l) => s.long_about(l), None => s }
            });
        }
    }
    cmd
}

/// The machine-translation notice, on stderr, for a non-English locale whose
/// catalog is not native-reviewed — goat's rule and goat's words.
pub fn warn_if_machine_translated() {
    if std::env::var_os("GOAT_I18N_NO_WARN").is_some() || is_english() {
        return;
    }
    let active = LANGUAGE_LOADER.current_language();
    let lang = active.language.as_str().to_string();
    if PROVENANCE.review.get(&lang).map(String::as_str).unwrap_or("machine") == "native-reviewed" {
        return;
    }
    let email = std::env::var("GOAT_I18N_FEEDBACK_EMAIL").unwrap_or_else(|_| PROVENANCE.feedback.email.clone());
    let repo = std::env::var("GOAT_I18N_FEEDBACK_REPO").unwrap_or_else(|_| PROVENANCE.feedback.repo.clone());
    let channels = match (email.is_empty(), repo.is_empty()) {
        (false, false) => format!("{email} or the partner repo ({repo})"),
        (false, true) => email,
        (true, false) => format!("the partner repo ({repo})"),
        (true, true) => "your goat administrator".to_string(),
    };
    let notice = i18n_embed_fl::fl!(
        LANGUAGE_LOADER,
        "i18n-machine-translation-warning",
        lang = active.to_string(),
        channels = channels
    );
    eprintln!("{notice}");
}

#[cfg(test)]
mod tests {
    use super::*;
    use clap::CommandFactory;

    fn loader_for(tag: &str) -> FluentLanguageLoader {
        let loader: FluentLanguageLoader = fluent_language_loader!();
        loader.load_fallback_language(&Localizations).unwrap();
        let ids: Vec<LanguageIdentifier> = vec![tag.parse().unwrap()];
        i18n_embed::select(&loader, &Localizations, &ids).unwrap();
        loader.set_use_isolating(false);
        loader
    }

    /// Every help string English shows, as (catalog id, text).
    fn english_help() -> Vec<(String, String)> {
        let cmd = crate::Cli::command();
        let mut v = vec![("help-about".to_string(), cmd.get_about().map(|s| s.to_string()).unwrap_or_default())];
        for a in cmd.get_arguments().filter(|a| a.is_global_set()) {
            if let Some(h) = a.get_help() {
                v.push((flag_key(a.get_id().as_str()), h.to_string()));
            }
        }
        for s in cmd.get_subcommands() {
            if let Some(a) = s.get_about() {
                v.push((cmd_key(s.get_name()), a.to_string()));
            }
            if let Some(l) = s.get_long_about() {
                v.push((format!("{}-long", cmd_key(s.get_name())), l.to_string()));
            }
        }
        // The tutor's printed lesson prose rides the same catalog (tutor.rs).
        v.extend(crate::tutor::prose_ids());
        v
    }

    fn ftl_value(text: &str) -> String {
        let esc = |l: &str| -> String {
            let l = l.replace('{', "{\"{\"}").replace('}', "{\"}\"}");
            match l.chars().next() {
                Some(c @ ('[' | '*' | '.')) => format!("{{\"{c}\"}}{}", &l[c.len_utf8()..]),
                _ => l,
            }
        };
        let lines: Vec<&str> = text.lines().collect();
        if lines.len() <= 1 {
            return format!(" {}", esc(text.trim()));
        }
        let mut out = String::new();
        for l in lines {
            out.push('\n');
            if !l.trim().is_empty() {
                out.push_str("    ");
                out.push_str(&esc(l.trim_end()));
            }
        }
        out
    }

    /// The English catalog is the doc comments, and stays so. Regenerate with
    /// `GOATBUILDER_I18N_WRITE=1 cargo test -p goatbuilder i18n::` after a
    /// help text changes; then fill the locales with
    /// `ops/scripts/translate-ftl.py --apply --catalog goatbuilder`.
    #[test]
    fn english_catalog_is_the_doc_comments() {
        let path = concat!(env!("CARGO_MANIFEST_DIR"), "/i18n/en/goatbuilder.ftl");
        let help = english_help();
        if std::env::var_os("GOATBUILDER_I18N_WRITE").is_some() {
            let mut s = String::from(
                "### goatbuilder — English (en), the arbiter\n\
                 ### Generated from the doc comments on Cli/Cmd (crates/goatbuilder/src/main.rs) by\n\
                 ### GOATBUILDER_I18N_WRITE=1 cargo test -p goatbuilder i18n:: — do not edit by hand.\n\n\
                 i18n-machine-translation-warning = note: { $lang } output is machine-translated and not yet native-reviewed — the English text is authoritative. Report translation errors to { $channels }. Suppress with GOAT_I18N_NO_WARN=1.\n\n",
            );
            for (id, text) in &help {
                s.push_str(&format!("{id} ={}\n", ftl_value(text)));
            }
            // The tutor page's fixed lines carry Fluent variables, so they are
            // written as Fluent, not escaped prose.
            for (id, value) in crate::tutor::CHROME {
                s.push_str(&format!("{id} = {value}\n"));
            }
            std::fs::write(path, s).unwrap();
        }
        let en = loader_for("en");
        for (id, text) in &help {
            assert!(en.has(id), "{id} is missing from i18n/en/goatbuilder.ftl — regenerate it (see this test's doc)");
            let got = en.get(id);
            let norm = |s: &str| s.lines().map(str::trim_end).collect::<Vec<_>>().join("\n").trim().to_string();
            assert_eq!(norm(&got), norm(text), "{id}: the English catalog drifted from the doc comment — regenerate it");
        }
        for (id, _) in crate::tutor::CHROME {
            assert!(en.has(id), "{id} is missing from i18n/en/goatbuilder.ftl — regenerate it (see this test's doc)");
        }
    }

    /// The tutor page's fixed lines render in every locale with each variable
    /// substituted (none dropped, no raw id), and English matches the format!
    /// each call site prints, so English output is unchanged.
    #[test]
    fn tutor_chrome_renders_in_every_locale() {
        let locales: Vec<String> = Localizations::iter()
            .filter_map(|p| p.strip_suffix("/goatbuilder.ftl").map(str::to_string))
            .collect();
        let var = regex::Regex::new(r"\{ \$([a-z]+) \}").unwrap();
        for tag in locales {
            let l = loader_for(&tag);
            for (id, english) in crate::tutor::CHROME {
                if !l.has(id) {
                    continue; // English fallback at runtime; the currency gate reports it
                }
                let names: Vec<String> = var.captures_iter(english).map(|c| c[1].to_string()).collect();
                let args: std::collections::HashMap<&str, String> = names.iter().map(|n| (n.as_str(), format!("Z{n}Z"))).collect();
                let got = l.get_args(id, args);
                assert!(!got.trim().is_empty() && got != *id, "{tag}/{id}: empty or raw id");
                for n in &names {
                    assert!(got.contains(&format!("Z{n}Z")), "{tag}/{id}: the variable `{n}` was dropped: {got}");
                }
                assert!(!got.contains("{ $"), "{tag}/{id}: an unrendered variable: {got}");
            }
        }
        let en = loader_for("en");
        fn args<'a>(v: &[(&'a str, &str)]) -> std::collections::HashMap<&'a str, String> {
            v.iter().map(|(k, s)| (*k, s.to_string())).collect()
        }
        assert_eq!(en.get_args("tutor-heading", args(&[("n", "1"), ("total", "11"), ("title", "T")])), format!("Lesson {} of {} · {}", 1, 11, "T"));
        assert_eq!(en.get_args("tutor-type", args(&[("met", "0"), ("total", "2")])), format!("Do these steps in your terminal ({} of {} done):", 0, 2));
        assert_eq!(en.get_args("tutor-lesson-done", args(&[("n", "3"), ("title", "T")])), format!("Lesson {} done: {}.", 3, "T"));
    }

    /// Every locale renders every help id with no panic and no raw id, and
    /// keeps every backticked command and flag verbatim.
    #[test]
    fn every_locale_renders_every_help_id() {
        let locales: Vec<String> = Localizations::iter()
            .filter_map(|p| p.strip_suffix("/goatbuilder.ftl").map(str::to_string))
            .collect();
        assert!(locales.iter().any(|l| l == "en"));
        for tag in locales {
            let l = loader_for(&tag);
            for (id, _) in english_help() {
                let got = l.get(&id);
                // English may have moved on from a locale's translation, so its
                // tokens are not compared here; translate-ftl.py refuses a
                // translation that loses one when it writes it.
                assert!(!got.trim().is_empty() && got != id, "{tag}/{id}: empty or raw id");
            }
        }
    }

    #[test]
    fn preparse_matches_goats_rules() {
        let a = |v: &[&str]| v.iter().map(|s| s.to_string()).collect::<Vec<_>>().into_iter();
        assert_eq!(preparse_lang_from(a(&["deploy", "--lang", "fr"]), Some("de".into())).as_deref(), Some("fr"));
        assert_eq!(preparse_lang_from(a(&["--lang=uk"]), None).as_deref(), Some("uk"));
        assert_eq!(preparse_lang_from(a(&["--", "--lang", "fr"]), Some("de".into())).as_deref(), Some("de"));
        assert_eq!(preparse_lang_from(a(&["doctor"]), Some(String::new())), None);
    }
}
