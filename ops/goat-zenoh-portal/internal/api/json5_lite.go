package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// json5LiteToJSON converts the subset of JSON5 our portal emits + that a
// reasonable user paste of a zenohd config carries into strict JSON, then
// returns the parsed result. Deliberately narrow:
//
//   - line comments (`// ...`) and block comments (`/* ... */`) stripped
//   - trailing commas before `}` / `]` removed
//   - unquoted object keys (identifier-ish: [A-Za-z_][A-Za-z0-9_-]*) quoted
//   - single-quoted strings rewritten to double-quoted (with the obvious
//     escape fixups)
//
// What it does NOT support: hex/octal/Infinity/NaN numerics, multi-line
// string-continuation, exotic identifier chars (Unicode IDs). These are
// vanishingly rare in zenohd configs; if a paste trips them, the surfacing
// "json5_parse_error" finding tells the user where.
//
// Returns the strict-JSON byte slice (mostly useful for debugging) along
// with the decoded tree. Errors are syntactic — semantic checks run after.
func json5LiteToJSON(src string) (jsonBytes []byte, decoded map[string]any, err error) {
	stripped, err := stripCommentsAndStrings(src)
	if err != nil {
		return nil, nil, err
	}
	deTrailed := dropTrailingCommas(stripped)
	keyed := quoteUnquotedKeys(deTrailed)

	var out map[string]any
	if err := json.Unmarshal([]byte(keyed), &out); err != nil {
		return []byte(keyed), nil, fmt.Errorf("after JSON5 normalization: %w", err)
	}
	return []byte(keyed), out, nil
}

// stripCommentsAndStrings walks the input character-by-character keeping
// track of whether we're inside a string literal. Comments outside strings
// are removed; comment-like sequences inside strings are preserved.
// Single-quoted strings are converted to double-quoted with internal
// double-quotes escaped (single quotes inside become bare).
func stripCommentsAndStrings(src string) (string, error) {
	var b strings.Builder
	b.Grow(len(src))
	i := 0
	n := len(src)
	for i < n {
		c := src[i]
		// Line comment.
		if c == '/' && i+1 < n && src[i+1] == '/' {
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment.
		if c == '/' && i+1 < n && src[i+1] == '*' {
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 >= n {
				return "", fmt.Errorf("unterminated block comment")
			}
			i += 2
			continue
		}
		// Double-quoted string — copy verbatim, honouring escapes.
		if c == '"' {
			b.WriteByte(c)
			i++
			for i < n {
				cc := src[i]
				b.WriteByte(cc)
				i++
				if cc == '\\' && i < n {
					b.WriteByte(src[i])
					i++
					continue
				}
				if cc == '"' {
					break
				}
			}
			continue
		}
		// Single-quoted string — convert to double-quoted.
		if c == '\'' {
			b.WriteByte('"')
			i++
			for i < n {
				cc := src[i]
				if cc == '\\' && i+1 < n {
					b.WriteByte(cc)
					b.WriteByte(src[i+1])
					i += 2
					continue
				}
				if cc == '\'' {
					i++
					b.WriteByte('"')
					break
				}
				if cc == '"' {
					b.WriteString(`\"`)
					i++
					continue
				}
				b.WriteByte(cc)
				i++
			}
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), nil
}

// dropTrailingCommas removes commas that immediately precede `}` or `]`,
// possibly with whitespace between. Strings are skipped (string literals
// arrived here as double-quoted from stripCommentsAndStrings).
func dropTrailingCommas(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	n := len(src)
	for i := 0; i < n; i++ {
		c := src[i]
		if c == '"' {
			b.WriteByte(c)
			i++
			for i < n {
				cc := src[i]
				b.WriteByte(cc)
				if cc == '\\' && i+1 < n {
					b.WriteByte(src[i+1])
					i += 2
					continue
				}
				if cc == '"' {
					break
				}
				i++
			}
			continue
		}
		if c == ',' {
			j := i + 1
			for j < n && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
				j++
			}
			if j < n && (src[j] == '}' || src[j] == ']') {
				// Drop the comma; preserve the whitespace.
				b.WriteString(src[i+1 : j])
				i = j - 1
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// quoteUnquotedKeys finds object-key positions where an identifier-ish
// token is followed by `:` (with optional whitespace) and wraps the
// identifier in double quotes. String contents are skipped.
func quoteUnquotedKeys(src string) string {
	var b strings.Builder
	b.Grow(len(src) + 64)
	n := len(src)
	for i := 0; i < n; i++ {
		c := src[i]
		if c == '"' {
			b.WriteByte(c)
			i++
			for i < n {
				cc := src[i]
				b.WriteByte(cc)
				if cc == '\\' && i+1 < n {
					b.WriteByte(src[i+1])
					i += 2
					continue
				}
				if cc == '"' {
					break
				}
				i++
			}
			continue
		}
		if isKeyStart(rune(c)) {
			j := i
			for j < n && isKeyPart(rune(src[j])) {
				j++
			}
			// Look past whitespace for a `:`. If found, this is a key.
			k := j
			for k < n && (src[k] == ' ' || src[k] == '\t') {
				k++
			}
			if k < n && src[k] == ':' && !isReservedLiteral(src[i:j]) {
				b.WriteByte('"')
				b.WriteString(src[i:j])
				b.WriteByte('"')
				b.WriteString(src[j:k])
				b.WriteByte(':')
				i = k
				continue
			}
			b.WriteString(src[i:j])
			i = j - 1
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isKeyStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isKeyPart(r rune) bool {
	return r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// pythonLiteralOccurrence records one Python-style boolean/null literal
// (`True` / `False` / `None`) found outside strings and comments. Line is
// 1-indexed.
type pythonLiteralOccurrence struct {
	Token string
	Line  int
}

// sniffPythonLiterals walks src and replaces bare `True` / `False` /
// `None` tokens (outside strings and comments) with their lowercase JSON5
// equivalents (`true` / `false` / `null`). Returns the normalized source
// plus a per-occurrence record so the lint handler can surface a
// dedicated finding for each one — the user's actual paste keeps parsing,
// the semantic rules still run, AND the user learns about the syntactic
// drift. Closes the gap where a paste-from-Python carried `True` and
// the whole lint surface collapsed into a single generic parse error.
func sniffPythonLiterals(src string) (normalized string, occurrences []pythonLiteralOccurrence) {
	var b strings.Builder
	b.Grow(len(src))
	line := 1
	n := len(src)
	i := 0
	for i < n {
		c := src[i]
		if c == '\n' {
			b.WriteByte(c)
			i++
			line++
			continue
		}
		// Line comment: copy through to newline.
		if c == '/' && i+1 < n && src[i+1] == '/' {
			for i < n && src[i] != '\n' {
				b.WriteByte(src[i])
				i++
			}
			continue
		}
		// Block comment: copy through closing `*/`, count interior newlines.
		if c == '/' && i+1 < n && src[i+1] == '*' {
			b.WriteByte(src[i])
			b.WriteByte(src[i+1])
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				if src[i] == '\n' {
					line++
				}
				b.WriteByte(src[i])
				i++
			}
			if i+1 < n {
				b.WriteByte(src[i])
				b.WriteByte(src[i+1])
				i += 2
			}
			continue
		}
		// Double-quoted string: copy verbatim, count interior newlines.
		if c == '"' {
			b.WriteByte(c)
			i++
			for i < n {
				cc := src[i]
				b.WriteByte(cc)
				i++
				if cc == '\\' && i < n {
					if src[i] == '\n' {
						line++
					}
					b.WriteByte(src[i])
					i++
					continue
				}
				if cc == '"' {
					break
				}
				if cc == '\n' {
					line++
				}
			}
			continue
		}
		// Single-quoted string: same posture, copy verbatim. The later
		// preprocessor rewrites these to double-quoted before JSON parses.
		if c == '\'' {
			b.WriteByte(c)
			i++
			for i < n {
				cc := src[i]
				b.WriteByte(cc)
				i++
				if cc == '\\' && i < n {
					if src[i] == '\n' {
						line++
					}
					b.WriteByte(src[i])
					i++
					continue
				}
				if cc == '\'' {
					break
				}
				if cc == '\n' {
					line++
				}
			}
			continue
		}
		// Identifier-ish token at value position. Match Python literals
		// case-sensitively; we deliberately leave `TRUE` / `nil` / etc.
		// alone — only the three Python-canonical capitalizations are
		// common-enough copy-paste sources to warrant a dedicated rule.
		if isKeyStart(rune(c)) {
			j := i
			for j < n && isKeyPart(rune(src[j])) {
				j++
			}
			tok := src[i:j]
			switch tok {
			case "True":
				b.WriteString("true")
				occurrences = append(occurrences, pythonLiteralOccurrence{Token: tok, Line: line})
			case "False":
				b.WriteString("false")
				occurrences = append(occurrences, pythonLiteralOccurrence{Token: tok, Line: line})
			case "None":
				b.WriteString("null")
				occurrences = append(occurrences, pythonLiteralOccurrence{Token: tok, Line: line})
			default:
				b.WriteString(tok)
			}
			i = j
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), occurrences
}

// pythonToJSONLiteral maps the recognized Python tokens to their JSON5
// canonical equivalents — small helper kept next to the sniff so future
// additions live in one place.
func pythonToJSONLiteral(tok string) string {
	switch tok {
	case "True":
		return "true"
	case "False":
		return "false"
	case "None":
		return "null"
	}
	return tok
}

// isReservedLiteral protects `true` / `false` / `null` from being
// mis-quoted when they appear as bare values inside e.g. an object literal
// at the top level. The colon-lookahead alone is not enough because
// `{enabled:true,enabled2:false}` would otherwise mark `true` / `false`
// as keys.
func isReservedLiteral(s string) bool {
	switch s {
	case "true", "false", "null":
		return true
	}
	return false
}
