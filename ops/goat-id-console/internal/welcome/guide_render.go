package welcome

// Rendering for the role-differentiated guide: a light index per role, and a
// deep dive per topic. See guide_model.go for why the model is shaped this way.

import (
	"html/template"
	"strings"
)

// GuidePage is the resolved input for one render.
type GuidePage struct {
	Goatnet string
	Tagline string
	Catalog Catalog
	Role    Role
	// Topic is the deep dive being shown; empty renders the role index.
	Topic *Topic
	// TopicLinks are the deep dives on this role's index, resolved in order.
	TopicLinks []Topic
	// Repos are the catalog entries for this role, with links composed if a
	// base is configured.
	Repos []RepoLink
	// ReposRest is the remainder of the catalog, rendered after Repos under
	// its own heading. The whole series shows for every role — the role list
	// only decides the order.
	ReposRest []RepoLink
	// RepoBase is the org base for repo links, e.g. "https://github.com/usareur-af".
	// Empty renders names WITHOUT links plus the ask-an-admin note — repos get
	// renamed and moved between orgs, and a dead link teaches readers the docs
	// are stale faster than anything else.
	RepoBase string
	Roles    []Role
	// RoleTotal is the path's total cost, already humanized for the template.
	RoleTotal string
}

// RepoLink is a catalog entry with its resolved URL (may be empty).
type RepoLink struct {
	RepoRef
	URL string
}

// BuildGuidePage resolves everything the template needs.
func BuildGuidePage(cat Catalog, roleKey, topicKey, repoBase string) GuidePage {
	// The credential steps and the bundle section describe how THIS goatnet
	// issues credentials, which is config, not copy. Resolve both from the
	// catalog before anything renders, so the path step and the deep dive it
	// links to can never disagree.
	role := withCredentialFlow(RoleByKey(roleKey), cat.MintOnCollect)
	p := GuidePage{
		Goatnet: DisplayName(cat.Goatnet), Tagline: cat.Tagline,
		Catalog: cat, Role: role, RepoBase: strings.TrimRight(repoBase, "/"),
		Roles: Roles, RoleTotal: HumanMins(role.TotalMins()),
	}
	for _, tk := range role.Topics {
		if t, ok := Topics[tk]; ok {
			p.TopicLinks = append(p.TopicLinks, t)
		}
	}
	for _, r := range ReposFor(role) {
		l := RepoLink{RepoRef: r}
		if p.RepoBase != "" {
			l.URL = p.RepoBase + "/" + r.Slug
		}
		p.Repos = append(p.Repos, l)
	}
	for _, r := range OtherReposFor(role) {
		l := RepoLink{RepoRef: r}
		if p.RepoBase != "" {
			l.URL = p.RepoBase + "/" + r.Slug
		}
		p.ReposRest = append(p.ReposRest, l)
	}
	if tk := strings.ToLower(strings.TrimSpace(topicKey)); tk != "" {
		if t, ok := Topics[tk]; ok {
			// COPY the sections before swapping: Topics is a package var shared
			// by every request, and an in-place write would rewrite the deep
			// dive for whichever goatnet asked last.
			secs := make([]Section, len(t.Sections))
			copy(secs, t.Sections)
			var built []Section
			for i := range secs {
				switch secs[i].ID {
				case "bundle":
					built = append(built, BundleSectionFor(cat.MintOnCollect))
				case "egress-addresses":
					built = append(built, egressSections(cat)...)
				default:
					built = append(built, secs[i])
				}
			}
			secs = built
			t.Sections = secs
			p.Topic = &t
		}
	}
	return p
}

// ResolveLink turns a Link's catalog key into a URL for this goatnet. An
// unconfigured surface returns "" and the caller renders plain text — a guide
// whose whole purpose is working links must never print a dead one.
func (c Catalog) ResolveLink(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "portal":
		return c.PortalURL
	case "panoscope":
		return c.Panoscope
	case "flashlight":
		return c.Flashlight
	case "stethoscope":
		return c.Stethoscope
	case "microscope":
		return c.Microscope
	case "grafana":
		return c.Grafana
	case "map":
		return c.Map
	case "sandbox":
		return c.Sandbox
	case "docs":
		return c.Docs
	case "isitup":
		return c.IsItUp
	case "chat":
		return c.Chat
	case "get":
		return c.GetURL
	case "relayhealth":
		return c.RelayHealth
	case "adminconsole":
		return c.AdminConsole
	case "identity":
		return joinConsolePath(c.AdminConsole, "/identity")
	// Both SSH-key pages live ON the admin console, so they are derived rather
	// than configured — a goatnet cannot have one without the other, and a
	// separate group_var could only ever drift from it.
	case "sshkey":
		return joinConsolePath(c.AdminConsole, "/ssh-key")
	case "sshkeyqueue":
		return joinConsolePath(c.AdminConsole, "/onboarding/ssh-keys")
	case "operatorkey":
		return joinConsolePath(c.AdminConsole, "/operator-key")
	case "onboardingqueue":
		return joinConsolePath(c.AdminConsole, "/onboarding")
	case "permissions":
		return joinConsolePath(c.AdminConsole, "/permissions")
	case "auth":
		return c.AuthURL
	}
	return ""
}

// joinConsolePath appends a console path to the console base, returning empty
// when the base is empty so the caller renders "not configured on this goatnet"
// rather than a link to a bare path.
func joinConsolePath(base, path string) string {
	if strings.TrimSpace(base) == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + path
}

// ResolvedLink is a Link with its URL filled in (possibly empty).
type ResolvedLink struct {
	Label string
	URL   string
}

// SectionLinks resolves a section's links against this page's catalog. Used from
// the template so topics can stay static data while still pointing at the real
// surfaces of whichever goatnet is serving them.
func (p GuidePage) SectionLinks(sec Section) []ResolvedLink {
	var out []ResolvedLink
	for _, l := range sec.Links {
		if l.URL != "" {
			out = append(out, ResolvedLink{Label: l.Label, URL: l.URL})
			continue
		}
		out = append(out, ResolvedLink{Label: l.Label, URL: p.Catalog.ResolveLink(l.Key)})
	}
	return out
}

// RenderGuidePage renders either the role index or one deep dive.
func RenderGuidePage(p GuidePage) (string, error) {
	var b strings.Builder
	if err := guidePageTmpl.Execute(&b, p); err != nil {
		return "", err
	}
	return b.String(), nil
}

var guidePageTmpl = template.Must(template.New("guidepage").Parse(guidePageHTML))

//nolint:misspell // keep "organisations" as spelled in the original copy
const guidePageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Goatnet}} guide — {{if .Topic}}{{.Topic.Title}}{{else}}{{.Role.Title}}{{end}}</title>
<style>
  :root{--bg:#fbfbfc;--card:#fff;--ink:#12161c;--ink2:#3c4654;--ink3:#6b7688;
    --rule:#d9dee6;--rule2:#b6bfcc;--accent:#1f5f8b;--accent2:#e8f0f6;
    --clock:#7a5a12;--clock2:#f6eeda;--warn:#9a2f22;--warn2:#f8e7e4;
    --ok:#14663f;--ok-soft:#e6f2ec}
  @media(prefers-color-scheme:dark){:root{--bg:#0e1216;--card:#161c23;--ink:#eef2f6;
    --ink2:#b6c1ce;--ink3:#8794a5;--rule:#262f3a;--rule2:#3a4653;--accent:#6fb2dd;
    --accent2:#14242f;--clock:#d9b662;--clock2:#241d0e;--warn:#e88b7c;--warn2:#2a1614;
    --ok:#5fc493;--ok-soft:#11241c}}
  *{box-sizing:border-box}
  body{margin:0;padding:0 1.25rem 5rem;background:var(--bg);color:var(--ink);
    font:16px/1.62 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;-webkit-font-smoothing:antialiased}
  .wrap{max-width:54rem;margin:0 auto}
  header{border-bottom:2px solid var(--rule2);padding:2.25rem 0 1.1rem;margin-bottom:1.5rem}
  .eyebrow{font-family:ui-monospace,Menlo,monospace;font-size:.71rem;letter-spacing:.13em;
    text-transform:uppercase;color:var(--ink3);margin:0 0 .7rem}
  h1{font-size:clamp(1.55rem,4vw,2.1rem);line-height:1.16;margin:0 0 .5rem;text-wrap:balance}
  .standfirst{color:var(--ink2);margin:0;max-width:40rem}
  nav.roles{display:flex;flex-wrap:wrap;gap:.4rem;margin:1.1rem 0 0}
  nav.roles a{font-size:.82rem;padding:.24rem .62rem;border:1px solid var(--rule2);
    border-radius:2px;text-decoration:none;color:var(--ink2)}
  nav.roles a.on{background:var(--accent);border-color:var(--accent);color:#fff}
  h2{font-size:1.2rem;margin:2.1rem 0 .3rem;padding-top:.9rem;border-top:1px solid var(--rule2);text-wrap:balance}
  h3{font-size:.77rem;letter-spacing:.1em;text-transform:uppercase;color:var(--ink3);margin:1.5rem 0 .45rem}
  p,li{max-width:40rem}p{margin:0 0 .8rem}
  code{font-family:ui-monospace,Menlo,monospace;font-size:.86em;background:var(--accent2);
    padding:.1em .34em;border-radius:2px;word-break:break-word}
  pre{background:var(--card);border:1px solid var(--rule);border-left:3px solid var(--accent);
    padding:.75rem .95rem;overflow-x:auto;margin:0 0 .9rem}
  pre code{background:none;padding:0;font-size:.85rem}
  a{color:var(--accent);text-underline-offset:2px}
  a:focus-visible,:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
  ol.path{list-style:none;counter-reset:s;padding:0;margin:0}
  ol.path>li{counter-increment:s;position:relative;padding:0 0 1.15rem 2.4rem;border-left:2px solid var(--rule2);margin-left:.6rem}
  ol.path>li:last-child{border-left-color:transparent;padding-bottom:0}
  ol.path>li::before{content:counter(s);position:absolute;left:-.85rem;top:0;
    width:1.7rem;height:1.7rem;border-radius:50%;background:var(--accent);color:#fff;
    display:grid;place-items:center;font-size:.8rem;font-variant-numeric:tabular-nums}
  ol.path h4{margin:.05rem 0 .3rem;font-size:1rem}
  .clock,.warn{border:1px solid;padding:.7rem .9rem;margin:.6rem 0 .9rem;max-width:40rem}
  .clock{border-color:var(--clock);background:var(--clock2)}
  .warn{border-color:var(--warn);background:var(--warn2)}
  .tag{font-family:ui-monospace,Menlo,monospace;font-size:.66rem;letter-spacing:.1em;
    text-transform:uppercase;display:block;margin-bottom:.28rem}
  .clock .tag{color:var(--clock)}.warn .tag{color:var(--warn)}
  .clock p,.warn p{margin:0 0 .35rem}.clock p:last-child,.warn p:last-child{margin:0}
  .cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(15rem,1fr));gap:1px;
    background:var(--rule);border:1px solid var(--rule);margin:.6rem 0 1rem}
  .cards>*{min-width:0}
  .cards a{background:var(--card);padding:.85rem .95rem;text-decoration:none;display:block;
    color:var(--ink);min-width:0;overflow-wrap:anywhere}
  .cards a strong{display:block;margin-bottom:.22rem;color:var(--accent)}
  .cards a span{font-size:.87rem;color:var(--ink2)}
  .repo{border:1px solid var(--rule);background:var(--card);padding:.8rem .95rem;margin:0 0 .6rem}
  .repo .slug{font-family:ui-monospace,Menlo,monospace;font-size:.8rem;color:var(--ink3);display:block;margin-top:.25rem}
  .pick{margin:1.15rem 0 0}
  .pick .lbl{display:block;font-size:.72rem;letter-spacing:.11em;text-transform:uppercase;
    color:var(--ink3);margin-bottom:.42rem}
  .pick .hint{display:block;font-size:.85rem;color:var(--ink2);margin-top:.45rem;max-width:38rem}
  .badges{display:flex;flex-wrap:wrap;gap:.32rem;margin:.5rem 0 0;min-width:0;max-width:100%}
  .b{font-size:.68rem;letter-spacing:.04em;padding:.14em .5em;border:1px solid var(--rule2);
    border-radius:999px;color:var(--ink2);background:var(--bg);
    /* wrap by default: an audience label can be a whole phrase, and nowrap on it
       pushed the badge out of its card. min-width:0 lets it shrink inside flex. */
    white-space:normal;overflow-wrap:anywhere;min-width:0;max-width:100%}
  .b.ess{border-color:var(--accent);color:var(--accent);font-weight:600}
  .b.who{border-style:dashed}
  /* the numeric badges are short and read badly broken, so they alone keep nowrap */
  .b.time{font-variant-numeric:tabular-nums;white-space:nowrap}
  .b.cat{border-color:var(--accent);color:var(--accent);background:var(--accent2)}
  /* Audience rides ON the heading as a chip rather than a sentence beneath it. */
  h2.sec{display:flex;flex-wrap:wrap;align-items:baseline;gap:.55rem}
  .for{font-size:.68rem;font-weight:400;letter-spacing:.04em;color:var(--ink2);
    border:1px dashed var(--rule2);border-radius:999px;padding:.12em .55em;white-space:normal}
  /* The three parts of a trap are DISTINCT surfaces, so the reader can find the
     scenario or the fix without reading the prose between them. The quiet marker
     replaces the shouted "THE SITUATION:" that used to sit inside the sentence. */
  .golinks{display:flex;flex-wrap:wrap;gap:.45rem;margin:.15rem 0 .9rem;max-width:40rem}
  .go{display:inline-block;font-size:.86rem;padding:.28em .7em;border:1px solid var(--accent);
    border-radius:3px;text-decoration:none;color:var(--accent);background:var(--accent2)}
  .go.off{border-style:dashed;border-color:var(--rule2);color:var(--ink3);background:none}
  .scenario,.instead{border-left:3px solid;padding:.6rem .9rem;margin:.55rem 0 .85rem;max-width:40rem}
  .scenario{border-color:var(--rule2);background:var(--card);color:var(--ink2)}
  .instead{border-color:var(--ok);background:var(--ok-soft)}
  .scenario p,.instead p{margin:0}
  .mark{display:block;font-size:.64rem;letter-spacing:.11em;text-transform:uppercase;
    margin-bottom:.3rem;font-family:ui-monospace,Menlo,monospace}
  .scenario .mark{color:var(--ink3)}
  .instead .mark{color:var(--ok);font-weight:600}
  .mark.os{color:var(--ink3);margin-top:.6rem}
  .total{background:var(--card);border:1px solid var(--rule);padding:.7rem .9rem;
    margin:.4rem 0 1.1rem;max-width:40rem;font-size:.93rem}
  .stepmin{font-size:.72rem;font-weight:400;color:var(--ink3);font-variant-numeric:tabular-nums;
    border:1px solid var(--rule2);border-radius:999px;padding:.1em .5em;margin-left:.35rem;white-space:nowrap}
  footer{margin-top:2.8rem;padding-top:1rem;border-top:1px solid var(--rule);color:var(--ink3);font-size:.85rem}
</style>
</head>
<body><div class="wrap">

<header>
  <p class="eyebrow">{{.Goatnet}}{{if .Tagline}} · {{.Tagline}}{{end}} · onboarding guide</p>
  {{if .Topic}}
    <h1>{{.Topic.Title}}</h1>
    {{if .Topic.Standfirst}}<p class="standfirst">{{.Topic.Standfirst}}</p>{{end}}
    <span class="badges">
      {{if .Topic.Essential}}<span class="b ess">Essential</span>{{else}}<span class="b">Optional</span>{{end}}
      {{if .Topic.Audience}}<span class="b who">{{.Topic.Audience}}</span>{{end}}
      {{if .Topic.ReadMin}}<span class="b time">{{.Topic.ReadMin}} min read</span>{{end}}
      {{if .Topic.DoMin}}<span class="b time">+{{.Topic.DoMin}} min to do it</span>{{end}}
    </span>
    <nav class="roles"><a href="?role={{.Role.Key}}">&larr; back to the {{.Role.Title}} path</a></nav>
  {{else}}
    <h1>{{.Role.Title}}</h1>
    <p class="standfirst">{{.Role.OneLine}}</p>
    <div class="pick">
      <span class="lbl">Choose the role that matches your work</span>
      <nav class="roles">
        {{$cur := .Role.Key}}{{range .Roles}}<a href="?role={{.Key}}"{{if eq .Key $cur}} class="on"{{end}}>{{.Title}}</a>{{end}}
      </nav>
      <span class="hint">Every view is readable by anyone — pick another if you want to see what someone else is asked to do.</span>
    </div>
  {{end}}
</header>

{{if .Topic}}
  {{range .Topic.Sections}}
    <h2 class="sec">{{.Heading}}{{if .For}} <span class="for">{{.For}}</span>{{end}}</h2>
    {{if .Scenario}}<div class="scenario"><span class="mark">When this happens</span><p>{{.Scenario}}</p></div>{{end}}
    {{range .Paras}}<p>{{.}}</p>{{end}}
    {{$ls := $.SectionLinks .}}{{if $ls}}<p class="golinks">
      {{range $ls}}{{if .URL}}<a class="go" href="{{.URL}}">{{.Label}} &rarr;</a>{{else}}<span class="go off">{{.Label}} — not configured on this goatnet</span>{{end}}{{end}}
    </p>{{end}}
    {{if .Instead}}<div class="instead"><span class="mark">Do this</span><p>{{.Instead}}</p></div>{{end}}
    {{if .Pre}}{{if .WinPre}}<span class="mark os">macOS / Linux</span>{{end}}<pre><code>{{.Pre}}</code></pre>{{end}}
    {{if .WinPre}}<span class="mark os">Windows (administrator Command Prompt)</span><pre><code>{{.WinPre}}</code></pre>{{end}}
    {{with .Clock}}<div class="clock">{{if .Window}}<span class="tag">Time-sensitive</span>
      <p><strong>{{.What}}</strong> lasts {{.Window}}.</p>
      <p>If it has already lapsed: {{.Recover}}.</p>{{else}}<span class="tag">One-time</span>
      <p><strong>{{.What}}</strong> {{.Limit}}.</p>
      <p>If you have already used it: {{.Recover}}.</p>{{end}}</div>{{end}}
    {{if .Warn}}<div class="warn"><span class="tag">Watch out</span><p>{{.Warn}}</p></div>{{end}}
  {{end}}

  {{if eq .Topic.Key "repos"}}
    {{range .Repos}}{{template "repocard" .}}{{end}}
    {{if .ReposRest}}
      <h3>The rest of the series</h3>
      <p>Not picked for the {{.Role.Title}} path, but the series is one set and every repository in it is open to the same community — one of these may be exactly what you are building today.</p>
      {{range .ReposRest}}{{template "repocard" .}}{{end}}
    {{end}}
    <div class="warn"><span class="tag">Access</span>
      <p>If a repository will not open for you, you probably do not have access yet — ask an admin rather than assuming it is gone.</p>
      <p>Repositories are also occasionally renamed or moved between organisations. The name above is what to ask for; if it does not resolve, ask where it lives now.</p></div>
  {{end}}
{{else}}
  <p class="total"><strong>Your path:</strong> {{len .Role.Path}} steps, {{.RoleTotal}} end to end.
     Each step is timed below, so you can take one in a gap rather than clearing an afternoon.
     The deep dives are extra and each says its own cost.</p>
  <ol class="path">
    {{range .Role.Path}}
      <li>
        <h4>{{.Title}}{{if .Mins}} <span class="stepmin">~{{.Mins}} min</span>{{end}}</h4>
        <p>{{.Body}}</p>
        {{with .Clock}}<div class="clock">{{if .Window}}<span class="tag">Time-sensitive</span>
          <p><strong>{{.What}}</strong> lasts {{.Window}}.</p>
          <p>If it has already lapsed: {{.Recover}}.</p>{{else}}<span class="tag">One-time</span>
          <p><strong>{{.What}}</strong> {{.Limit}}.</p>
          <p>If you have already used it: {{.Recover}}.</p>{{end}}</div>{{end}}
        {{if .Topic}}<p><a href="?role={{$.Role.Key}}&amp;topic={{.Topic}}">Read the detail &rarr;</a></p>{{end}}
      </li>
    {{end}}
  </ol>

  <h2>Go deeper</h2>
  <div class="cards">
    {{$r := .Role.Key}}{{range .TopicLinks}}
      <a href="?role={{$r}}&amp;topic={{.Key}}"><strong>{{.Title}}</strong><span>{{.Standfirst}}</span>
        <span class="badges">
          {{if .Essential}}<span class="b ess">Essential</span>{{else}}<span class="b">Optional</span>{{end}}
          {{if .Audience}}<span class="b who">{{.Audience}}</span>{{end}}
          {{if .ReadMin}}<span class="b time">{{.ReadMin}} min read</span>{{end}}
          {{if .DoMin}}<span class="b time">+{{.DoMin}} min to do it</span>{{end}}
        </span></a>
    {{end}}
  </div>
{{end}}

<footer>
  <p>Written for the <strong>{{.Role.Title}}</strong> role on {{.Goatnet}}. This page holds nothing specific to you and carries no credentials — those are in your welcome email. Every role's view and every topic is readable by anyone who can reach this page; pick another role above if you want to see what someone else is asked to do.</p>
</footer>

</div></body>
</html>
{{define "repocard"}}<div class="repo">
  <strong>{{if .URL}}<a href="{{.URL}}">{{.Title}}</a>{{else}}{{.Title}}{{end}}</strong>
  <p style="margin:.3rem 0 0">{{.What}}</p>
  <span class="badges">
    {{if .Category}}<span class="b cat">{{.Category}}</span>{{end}}
    {{if .ReadMin}}<span class="b time">{{.ReadMin}} min read</span>{{end}}
    {{if .DoMin}}<span class="b time">+{{.DoMin}} min to run it</span>{{end}}
  </span>
  <span class="slug">{{.Slug}}</span>
</div>{{end}}`
