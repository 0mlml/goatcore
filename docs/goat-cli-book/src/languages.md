# Languages

goat speaks your language. Its human-facing text — errors, the `doctor` and `status` readouts, the `--help` screens — is translated into **29 languages**.

## Choosing a language

goat picks a language in this order:

1. the `--lang` flag (`goat --lang fr doctor`),
2. the `GOAT_LANG` environment variable (`export GOAT_LANG=uk`),
3. your operating system's locale,
4. English.

```bash
goat --lang de doctor      # just this command, in German
export GOAT_LANG=fr        # everything, in French, for this shell
```

English is the authoritative source: if a phrase hasn't been translated yet, goat shows the English rather than a blank.

## What stays the same in every language

The **machine surface never changes**. `--json` field values, error codes (`E_OFF_MESH`, …), exit codes, command and flag names, and contract tokens (paths, keys, URLs) are byte-for-byte identical no matter the language. Translate the prose, not the contract — so your scripts work the same for every operator.

## A note on machine-translated text

Most translations are machine-produced and awaiting native-speaker review. When you run goat in one of those languages, it prints a short, one-time notice on standard error telling you the text is machine-translated, that the English is authoritative, and where to report a translation error. Suppress it with `GOAT_I18N_NO_WARN=1`.

If you speak one of these languages and spot something off, that report is genuinely welcome — it's how the catalogs get better.

## And one for fun

`goat --lang tlh` renders the marquee lines in **Klingon**. It's an opt-in easter egg — never selected automatically — and, like every other locale, it can't touch the machine surface. Qapla'.
