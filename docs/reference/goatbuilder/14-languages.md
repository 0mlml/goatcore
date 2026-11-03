# 14. Languages

goatbuilder's help, its error explanations and the tutor's lessons are
available in 29 languages. Everything a program or a person types stays the
same in every language.

| Field | Value |
|---|---|
| Languages | bg, cnr, cs, da, de, el, en, es, et, fi, fr, hr, hu, is, it, lb, lt, lv, mk, nb, nl, pl, pt, ro, sk, sl, sq, sv, tr, uk |
| English source | `crates/goatbuilder/i18n/en/goatbuilder.ftl`, generated from the binary's help text and the tutor's lessons |
| Review state | every non-English catalog is machine-translated; a native-speaker review is recorded per language in `crates/goat-cli/i18n-provenance.toml` |
| Design | [goat-cli-i18n.md](../../design/goat-cli-i18n.md) §"CLI catalog currency, the error catalog, and goatbuilder" |

## Choosing a language

The first of these that is set wins:

1. `--lang <TAG>` on the command line, such as `--lang fr` or `--lang uk`
2. the `GOAT_LANG` environment variable
3. the operating system's locale
4. English

```bash
goatbuilder --lang fr tutor
GOAT_LANG=uk goatbuilder explain GB-007
```

## What is translated, and what never is

| Translated | Never translated |
|---|---|
| goatbuilder's help: the summary, every verb and every flag's description | commands, verbs, subcommands and flags |
| error explanations: each entry's title, what happened and likely causes | error codes (`GB-*`, `X-*`, `E_*`), fix commands, read paths, exit codes |
| the tutor's printed lesson page: lesson titles, lesson text and the page's fixed lines | the tutor's report rows, step labels, hints, and the commands each lesson asks you to run |
| the `goat` CLI's own messages | report rows, verdict lines and every `-o json` or `-o yaml` field |
| | file paths, refs, digests, and anything inside backticks |

An error explanation in another language prints the translated line and then
the binding English, marked `(en: …)`. A non-English run first prints, in
English, a one-line notice that the text is machine-translated and the English
is authoritative; `GOAT_I18N_NO_WARN=1` suppresses it.

```text
GB-030 — aucun hôte du groupe dans les inventaires chargés
  (en: no host of the group is in the loaded inventories)
```

Verb output stays English on purpose. The parity gates compare it line for line
with the scripts, and pipelines parse it. The explanation of a refusal reaches
the reader in their language through `explain`.

## Keeping translations current

A missing translation fails CI. `ops/scripts/translate-ftl.py --check` lists
every English message missing from any shipped language and exits 1; it runs
on every pull request that touches the goat CLI, goatbuilder or the error
catalog. After changing help text or a lesson, regenerate the English source
and fill the other languages:

```bash
GOATBUILDER_I18N_WRITE=1 cargo test -p goatbuilder i18n::
python3 ops/scripts/translate-ftl.py --catalog goatbuilder --apply
python3 ops/scripts/translate-ftl.py --catalog all --check
```

`--apply` refuses any translation that changes a backticked token or a
placeholder.
