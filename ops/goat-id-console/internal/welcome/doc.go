package welcome

import (
	"html"
	"regexp"
	"strings"
)

// doc is the welcome package as a document, not as a string. The renderer
// builds it with typed calls (title, h2, para, step, cmd, bullet, note, cred,
// link); Text() emits exactly the plain-text lines the renderer has always
// produced, and HTML() lays the same blocks out for a mail client. One source
// of content, two presentations — the text part is the record, the HTML part
// is what a person in Gmail or Outlook actually reads.
type doc struct {
	text   strings.Builder
	blocks []block
	notes  [][]string
}

type blockKind int

const (
	bTitle blockKind = iota
	bMeta
	bH2
	bPara   // prose lines, joined with spaces for HTML
	bStep   // "N) ..." item; text holds the item's lines, cmds its code lines
	bItem   // "  - " / "  N. " sub-item (unordered)
	bBullet // "  * " item
	bMono   // a command, address or credential line, shown verbatim
	bCred   // label: value credential
	bLink   // label: url surface row
	bNotes  // the lettered troubleshooting notes, in place
	bBlank
)

type block struct {
	kind   blockKind
	lines  []string
	label  string
	value  string
	indent int
}

func newDoc() *doc { return &doc{} }

// --- text side: every method writes the lines it is given, verbatim ---------

func (d *doc) raw(s string) { d.text.WriteString(s + "\n") }
func (d *doc) rule()        { d.raw("============================================================") }
func (d *doc) blank()       { d.raw(""); d.push(block{kind: bBlank}) }

func (d *doc) title(s string) { d.raw(s); d.push(block{kind: bTitle, lines: []string{s}}) }
func (d *doc) meta(s string)  { d.raw(s); d.push(block{kind: bMeta, lines: []string{s}}) }
func (d *doc) h2(s string)    { d.raw(s); d.push(block{kind: bH2, lines: []string{s}}) }

// para: a prose line. Consecutive para lines at the same indent form one
// paragraph in HTML; a change of indent (the renderer's way of starting a
// sub-paragraph without a blank line) starts a new one.
func (d *doc) para(s string) {
	d.raw(s)
	indent := len(s) - len(strings.TrimLeft(s, " "))
	if n := len(d.blocks); n > 0 && d.blocks[n-1].kind == bPara && d.blocks[n-1].indent == indent {
		d.blocks[n-1].lines = append(d.blocks[n-1].lines, strings.TrimSpace(s))
		return
	}
	d.push(block{kind: bPara, indent: indent, lines: []string{strings.TrimSpace(s)}})
}

// step: a numbered "N) ..." item. cont lines and cmd lines attach to it.
func (d *doc) step(s string) {
	d.raw(s)
	d.push(block{kind: bStep, lines: []string{strings.TrimSpace(stepPrefix.ReplaceAllString(strings.TrimSpace(s), ""))}})
}

// item: a "  - ..." or "  N. ..." sub-item.
func (d *doc) item(s string) {
	d.raw(s)
	t := strings.TrimSpace(s)
	t = itemPrefix.ReplaceAllString(t, "")
	d.push(block{kind: bItem, lines: []string{t}})
}

// bullet: a "  * ..." item (the time-sensitive callout).
func (d *doc) bullet(s string) {
	d.raw(s)
	d.push(block{kind: bBullet, lines: []string{strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "* "))}})
}

// cont: a continuation line of the last step / item / bullet / para.
func (d *doc) cont(s string) {
	d.raw(s)
	t := strings.TrimSpace(s)
	for i := len(d.blocks) - 1; i >= 0; i-- {
		switch d.blocks[i].kind {
		case bStep, bItem, bBullet, bPara:
			d.blocks[i].lines = append(d.blocks[i].lines, t)
			return
		case bMono, bCred, bLink:
			continue // a command between an item and its continuation
		default:
			i = -1
		}
	}
	d.push(block{kind: bPara, lines: []string{t}})
}

// cmd: a command, address, key or other line the reader copies verbatim.
func (d *doc) cmd(s string) {
	d.raw(s)
	t := strings.TrimSpace(s)
	if n := len(d.blocks); n > 0 && d.blocks[n-1].kind == bMono {
		d.blocks[n-1].lines = append(d.blocks[n-1].lines, t)
		return
	}
	d.push(block{kind: bMono, lines: []string{t}})
}

// cred: "label: value" on one text line; a labeled box in HTML.
func (d *doc) cred(indent, label, value string) { d.credSep(indent, label, ": ", value) }

// credSep is cred with the exact separator the text line has always carried.
func (d *doc) credSep(indent, label, sep, value string) {
	d.raw(indent + label + sep + value)
	d.push(block{kind: bCred, label: label, value: value})
}

// note: one troubleshooting entry — a short title (the symptom or the rule)
// and a body that says what to do. Text wraps the body under the title; HTML
// sets the title bold on its own line.
func (d *doc) note(title string, body ...string) {
	d.notes = append(d.notes, append([]string{title}, body...))
}

// notesBlock emits the collected notes where the renderer places them (after
// the guide pointer): the lettered text list, and a bNotes block the HTML
// renders at the same position, so both presentations keep the same order.
func (d *doc) notesBlock() {
	if len(d.notes) == 0 {
		return
	}
	d.rule()
	d.raw("IF SOMETHING DOES NOT WORK — notes")
	d.rule()
	d.raw("Nothing here is a step. Come back to it when a step above does not behave.")
	for k, n := range d.notes {
		d.raw("")
		d.raw("  " + string(rune('A'+k)) + ". " + n[0])
		for _, l := range n[1:] {
			d.raw("     " + l)
		}
	}
	d.raw("")
	d.push(block{kind: bNotes})
}

func (d *doc) push(b block) { d.blocks = append(d.blocks, b) }

func (d *doc) Text() string { return d.text.String() }

var (
	stepPrefix = regexp.MustCompile(`^\d+\)\s*`)
	itemPrefix = regexp.MustCompile(`^(-|\d+\.)\s+`)
	urlRe      = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `]+`)
)

// --- HTML side --------------------------------------------------------------

// HTML lays the blocks out for a mail client. Styles are inline because
// Gmail and Outlook strip or ignore most stylesheets; the palette is quiet
// (ink on white, one green accent for structure, amber for the deadline
// callout) so the credentials and commands are the loudest things on the page.
func (d *doc) HTML() string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` +
		esc(firstLine(d.blocks, bTitle)) + `</title></head>` +
		`<body style="margin:0;padding:0;background:#f3f5f2;">` +
		`<div style="max-width:660px;margin:0 auto;padding:28px 16px 40px;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:15px;line-height:1.55;color:#1b2621;">` +
		`<div style="background:#ffffff;border:1px solid #dde3de;border-radius:8px;padding:30px 34px;">` + "\n")

	// A section is everything from one h2 to the next. The deadline section
	// and the repeat banner render as callouts; the rest as plain sections.
	type section struct {
		title  string
		blocks []block
	}
	var secs []section
	cur := section{}
	for _, bl := range d.blocks {
		if bl.kind == bH2 {
			secs = append(secs, cur)
			cur = section{title: bl.lines[0]}
			continue
		}
		cur.blocks = append(cur.blocks, bl)
	}
	secs = append(secs, cur)

	for si, s := range secs {
		callout := ""
		switch {
		case strings.HasPrefix(s.title, "!!"):
			callout = "background:#fff7e8;border:1px solid #e8c98a;border-radius:6px;padding:14px 18px;margin:0 0 22px;"
		case strings.HasPrefix(s.title, "REPEAT"):
			callout = "background:#fdecec;border:1px solid #e3a0a0;border-radius:6px;padding:14px 18px;margin:0 0 22px;"
		}
		if callout != "" {
			b.WriteString(`<div style="` + callout + `">`)
			b.WriteString(`<p style="margin:0 0 8px;font-weight:700;font-size:14px;letter-spacing:.02em;">` + esc(strings.TrimLeft(s.title, "! ")) + "</p>\n")
		} else if s.title != "" {
			b.WriteString(`<h2 style="margin:` + map[bool]string{true: "0", false: "30px"}[si == 1] + ` 0 12px;padding-top:` + map[bool]string{true: "0", false: "18px"}[si == 1] + `;border-top:` + map[bool]string{true: "0", false: "2px solid #1f6f5b"}[si == 1] + `;font-size:16px;font-weight:700;color:#1f6f5b;">` + esc(s.title) + "</h2>\n")
		}
		d.writeBlocks(&b, s.blocks)
		if callout != "" {
			b.WriteString("</div>\n")
		}
	}

	b.WriteString("</div></div></body></html>\n")
	return b.String()
}

// writeBlocks renders a run of blocks, grouping steps into an ordered list,
// items and bullets into unordered lists, and attaching commands to the item
// they follow.
func (d *doc) writeBlocks(b *strings.Builder, blocks []block) {
	const (
		liStyle   = `style="margin:0 0 10px;"`
		monoStyle = `style="margin:8px 0 10px;padding:10px 12px;background:#1b2621;color:#e8efe9;border-radius:5px;font-family:ui-monospace,Menlo,Consolas,'Courier New',monospace;font-size:13px;line-height:1.5;white-space:pre-wrap;overflow-wrap:anywhere;"`
		credStyle = `style="margin:8px 0 12px;padding:10px 14px;background:#eef4f0;border-left:4px solid #1f6f5b;border-radius:4px;"` //nolint:gosec // G101: CSS for the credential box, not a credential
	)
	listOpen := "" // "ol" | "ul"
	closeList := func() {
		if listOpen != "" {
			b.WriteString("</" + listOpen + ">\n")
			listOpen = ""
		}
	}
	liOpen := false
	closeLi := func() {
		if liOpen {
			b.WriteString("</li>\n")
			liOpen = false
		}
	}
	openList := func(kind, style string) {
		if listOpen != kind {
			closeLi()
			closeList()
			b.WriteString("<" + kind + ` style="` + style + `">` + "\n")
			listOpen = kind
		}
	}
	for _, bl := range blocks {
		switch bl.kind {
		case bBlank:
			// Paragraph breaks are structural in HTML; nothing to emit.
		case bTitle:
			b.WriteString(`<h1 style="margin:0 0 4px;font-size:22px;line-height:1.3;font-weight:700;">` + esc(bl.lines[0]) + "</h1>\n")
		case bMeta:
			b.WriteString(`<p style="margin:0 0 20px;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12.5px;color:#5d6b65;">` + esc(bl.lines[0]) + "</p>\n")
		case bPara:
			closeLi()
			closeList()
			b.WriteString(`<p style="margin:0 0 12px;">` + inline(strings.Join(bl.lines, " ")) + "</p>\n")
		case bStep:
			openList("ol", "margin:0 0 14px;padding-left:24px;")
			closeLi()
			if len(bl.lines) > 1 && strings.Contains(bl.lines[0], ":  ") {
				b.WriteString("<li " + liStyle + ">" + inline(bl.lines[0]) + "<br>" + inline(strings.Join(bl.lines[1:], " ")))
			} else {
				b.WriteString("<li " + liStyle + ">" + inline(strings.Join(bl.lines, " ")))
			}
			liOpen = true
		case bItem:
			openList("ul", "margin:0 0 14px;padding-left:22px;")
			closeLi()
			b.WriteString("<li " + liStyle + ">" + inline(strings.Join(bl.lines, " ")))
			liOpen = true
		case bBullet:
			openList("ul", "margin:0;padding-left:20px;")
			closeLi()
			b.WriteString("<li " + liStyle + ">" + inline(strings.Join(bl.lines, " ")))
			liOpen = true
		case bMono:
			if !liOpen {
				closeList()
			}
			if len(bl.lines) == 1 && urlRe.FindString(bl.lines[0]) == bl.lines[0] {
				// An address on its own line is a link to open, not a command to type.
				b.WriteString(`<p style="margin:8px 0 12px;padding:10px 14px;background:#eef4f0;border-left:4px solid #1f6f5b;border-radius:4px;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:13.5px;overflow-wrap:anywhere;"><a href="` + esc(bl.lines[0]) + `" style="color:#1f6f5b;">` + esc(bl.lines[0]) + "</a></p>\n")
				break
			}
			b.WriteString("<pre " + monoStyle + ">" + linkifyMono(esc(strings.Join(bl.lines, "\n"))) + "</pre>\n")
		case bCred:
			if !liOpen {
				closeList()
			}
			b.WriteString("<div " + credStyle + `><span style="display:block;font-size:12px;letter-spacing:.05em;text-transform:uppercase;color:#5d6b65;">` + esc(bl.label) + `</span><span style="display:block;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:14px;overflow-wrap:anywhere;">` + linkifyColor(esc(bl.value), "#1f6f5b") + "</span></div>\n")
		case bNotes:
			closeLi()
			closeList()
			b.WriteString(`<h2 style="margin:30px 0 8px;padding-top:18px;border-top:2px solid #1f6f5b;font-size:16px;font-weight:700;color:#1f6f5b;">IF SOMETHING DOES NOT WORK — notes</h2>` + "\n")
			b.WriteString(`<p style="margin:0 0 12px;color:#5d6b65;">Nothing here is a step. Come back to it when a step above does not behave.</p>` + "\n")
			b.WriteString(`<ol type="A" style="margin:0 0 14px;padding-left:26px;color:#3d4a45;">` + "\n")
			for _, n := range d.notes {
				b.WriteString(`<li style="margin:0 0 12px;"><strong style="color:#1b2621;">` + inline(n[0]) + `</strong><br>` + inline(strings.Join(n[1:], " ")) + "</li>\n")
			}
			b.WriteString("</ol>\n")
		case bLink:
			closeLi()
			closeList()
			b.WriteString(`<p style="margin:0 0 6px;">` + esc(bl.label) + `<br><a href="` + esc(bl.value) + `" style="color:#1f6f5b;">` + esc(bl.value) + "</a></p>\n")
		}
	}
	closeLi()
	closeList()
}

func firstLine(blocks []block, k blockKind) string {
	for _, bl := range blocks {
		if bl.kind == k && len(bl.lines) > 0 {
			return bl.lines[0]
		}
	}
	return ""
}

func esc(s string) string { return html.EscapeString(s) }

// inline: escape prose, link URLs, and set `code` spans in monospace. URLs
// are found on the raw text, before escaping, so an entity such as &#34; can
// never be swallowed into a link.
func inline(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range urlRe.FindAllStringIndex(s, -1) {
		b.WriteString(codeSpans(esc(s[last:m[0]])))
		u := s[m[0]:m[1]]
		trim := strings.TrimRight(u, ".,;:)")
		b.WriteString(`<a href="` + esc(trim) + `" style="color:#1f6f5b;">` + esc(trim) + `</a>` + esc(u[len(trim):]))
		last = m[1]
	}
	b.WriteString(codeSpans(esc(s[last:])))
	return b.String()
}

// codeSpans sets `backticked` text in monospace, and the command that follows
// a "label:  " (two spaces) on a step line — the renderer's convention for an
// inline command such as "Check it installed:  netbird version".
func codeSpans(s string) string {
	s = backtickRe.ReplaceAllString(s, codeOpen+`$1</code>`)
	if i := strings.Index(s, ":  "); i > 0 && !strings.Contains(s[:i], "<code") {
		rest := s[i+3:]
		// Only when what follows reads as a command (no sentence punctuation
		// before the next clause), so a prose colon is left alone.
		if j := strings.IndexAny(rest, ".;"); j < 0 || j > 0 && !strings.Contains(rest[:j], " — ") {
			cmd, tail := rest, ""
			if j > 0 && j < len(rest) {
				// "netbird status -d It must say ..." — the command ends where
				// the next sentence starts (a capitalised word after a space).
				if k := capitalAfterSpace.FindStringIndex(rest); k != nil {
					cmd, tail = rest[:k[0]], rest[k[0]:]
				}
			}
			return s[:i+1] + " " + codeOpen + strings.TrimSpace(cmd) + "</code> " + tail
		}
	}
	return s
}

const codeOpen = `<code style="font-family:ui-monospace,Menlo,Consolas,monospace;font-size:13px;background:#eef1ee;padding:1px 5px;border-radius:3px;">`

var capitalAfterSpace = regexp.MustCompile(` [A-Z][a-z]`)

func linkifyMono(s string) string { return linkifyColor(s, "#9fd3bd") }

func linkifyColor(s, color string) string {
	return urlRe.ReplaceAllStringFunc(s, func(u string) string {
		return `<a href="` + u + `" style="color:` + color + `;">` + u + `</a>`
	})
}

var backtickRe = regexp.MustCompile("`([^`]+)`")
