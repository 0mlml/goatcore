package welcome

// What a person's own network must allow BEFORE they enroll, stated where they
// can read it before they have mesh access: the invite email, the public intake
// page, the public network-requirements page and the welcome email.
//
// It is built from the same three config facts as the guide's firewall topic
// (egress.go) — the management URL, the relay list handed to clients and the
// STUN hosts — so it cannot name a relay the goatnet does not run. It carries
// host NAMES and ports only, never resolved addresses: every name here is in
// public DNS already, and a public page must not print an internal address.
//
// The failure it exists for: a site that allows TCP 443 but blocks the relay
// port. NetBird then registers and reports "Connected" (control on 443 works)
// while every peer sits at "Connecting" — 0 peers, so no mesh name resolves
// and the login link never opens (zagros).

import (
	"html/template"
	"net/url"
	"sort"
	"strings"
)

// NetworkRequirements splits the outbound needs into the two kinds a network
// team treats differently: the control connection (normally already allowed,
// it is ordinary HTTPS) and the data connections (the ones sites block).
type NetworkRequirements struct {
	ControlHost  string
	ControlPorts []string // e.g. "TCP 443"
	DataHosts    []string // relays and STUN hosts, in config order
	DataPorts    []string // union over DataHosts, e.g. "TCP 33073", "UDP 3478", "UDP 33073"
	// DataRows keeps each data host's own ports for the page table. Host and
	// Ports only: the IPs field of EgressHost is never filled here.
	DataRows []EgressHost
}

// NetworkRequirementsFrom derives the requirements from config. ok is false
// when there is nothing useful to say: no management host, or no data host
// (a goatnet whose relay list is not configured).
func NetworkRequirementsFrom(mgmtURL string, relayAddrs, stunHosts []string) (NetworkRequirements, bool) {
	var n NetworkRequirements
	controlPort := ""
	if u, err := url.Parse(strings.TrimSpace(mgmtURL)); err == nil && u.Hostname() != "" {
		n.ControlHost = strings.ToLower(u.Hostname())
		p := u.Port()
		if p == "" {
			p = "443"
		}
		controlPort = "TCP " + p
		n.ControlPorts = []string{controlPort}
	}
	data := map[string]bool{}
	for _, h := range EgressHostsFrom(mgmtURL, relayAddrs, stunHosts) {
		var rest []string
		for _, p := range h.Ports {
			if h.Host == n.ControlHost && p == controlPort {
				continue
			}
			rest = append(rest, p)
		}
		if len(rest) == 0 {
			continue
		}
		n.DataHosts = append(n.DataHosts, h.Host)
		n.DataRows = append(n.DataRows, EgressHost{Host: h.Host, Ports: rest})
		for _, p := range rest {
			data[p] = true
		}
	}
	for p := range data {
		n.DataPorts = append(n.DataPorts, p)
	}
	sort.Slice(n.DataPorts, func(i, j int) bool { return portLess(n.DataPorts[i], n.DataPorts[j]) })
	return n, n.ControlHost != "" && len(n.DataHosts) > 0
}

// BlockedSymptom is what a person sees when only the control connection gets
// through. Stated identically on every surface so it can be searched for.
const BlockedSymptom = `NetBird shows "Connected" but 0 peers connected, and the login link does not open.`

// TestCommand is the one-line check a network team can run from the site
// before anyone enrolls: a TCP connect to the first relay on its relay port.
// Empty when no data host carries a TCP port.
func (n NetworkRequirements) TestCommand() string {
	if len(n.DataHosts) == 0 {
		return ""
	}
	for _, p := range n.DataPorts {
		if strings.HasPrefix(p, "TCP ") {
			return "nc -zv " + n.DataHosts[0] + " " + strings.TrimPrefix(p, "TCP ")
		}
	}
	return ""
}

// EmailLines renders the short "Before you start: your network" block for a
// plain-text email: what to allow, a test, the symptom, and where the full
// page is (pageURL; omitted when empty). Lines are kept under 80 columns.
func (n NetworkRequirements) EmailLines(goatnet, pageURL string) []string {
	lines := []string{
		"Before you start: your network",
		"  " + DisplayName(goatnet) + " needs these OUTBOUND connections from the computer you will use:",
		"    " + strings.Join(n.ControlPorts, ", ") + "  to  " + n.ControlHost,
	}
	lines = append(lines, wrapList("    "+strings.Join(n.DataPorts, ", ")+"  to  ", n.DataHosts, 78)...)
	lines = append(lines, "  Organisation networks often block the second kind. Ask your network team")
	if t := n.TestCommand(); t != "" {
		lines = append(lines, "  to check it before you begin:  "+t)
	} else {
		lines = append(lines, "  to check it before you begin.")
	}
	lines = append(lines, "  If blocked: NetBird says Connected with 0 peers; the login link won't open.")
	if pageURL != "" {
		lines = append(lines, "  Details for your network team: "+pageURL)
	}
	return lines
}

// wrapList joins items after prefix, continuing on indented lines so no line
// runs past width.
func wrapList(prefix string, items []string, width int) []string {
	var out []string
	cur := prefix
	indent := strings.Repeat(" ", len(prefix))
	for i, it := range items {
		piece := it
		if i < len(items)-1 {
			piece += ","
		}
		if cur != prefix && cur != indent && len(cur)+1+len(piece) > width {
			out = append(out, cur)
			cur = indent
		}
		if cur == prefix || cur == indent {
			cur += piece
		} else {
			cur += " " + piece
		}
	}
	return append(out, cur)
}

// NetworkPageURL is where the public network-requirements page lives, derived from a
// public intake URL (any path or query on it is dropped). Empty when the base
// does not parse to an http(s) host.
func NetworkPageURL(publicBase string) string {
	u, err := url.Parse(strings.TrimSpace(publicBase))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/network-requirements"
}

// RenderNetworkRequirementsPage is the public page a participant hands to their
// network team. Names and ports only; nothing on it is secret or internal.
func RenderNetworkRequirementsPage(goatnet string, n NetworkRequirements, ok bool) (string, error) {
	type row struct {
		Host, Ports, Purpose, Needed string
	}
	var rows []row
	if ok {
		rows = append(rows, row{n.ControlHost, strings.Join(n.ControlPorts, ", "), "Joining the network (control)", "Usually already allowed"})
		for _, r := range n.DataRows {
			purpose, needed := "Path discovery (STUN)", "Strongly recommended"
			for _, p := range r.Ports {
				if strings.HasPrefix(p, "TCP ") {
					purpose, needed = "Relayed connections between devices", "Required"
					break
				}
			}
			rows = append(rows, row{r.Host, strings.Join(r.Ports, ", "), purpose, needed})
		}
	}
	var b strings.Builder
	err := netReqTmpl.Execute(&b, map[string]any{
		"Name":    DisplayName(goatnet),
		"OK":      ok,
		"Rows":    rows,
		"Test":    n.TestCommand(),
		"Symptom": BlockedSymptom,
	})
	return b.String(), err
}

var netReqTmpl = template.Must(template.New("netreq").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Name}}: network requirements</title>
<style>
body{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;max-width:52rem;margin:0 auto;padding:24px 16px;line-height:1.5;color:#111;background:#fff}
h1{font-size:1.5rem;margin:0 0 .3rem}h2{font-size:1.1rem;margin:1.6rem 0 .4rem}
table{border-collapse:collapse;width:100%;font-size:.95rem}th,td{border:1px solid #bbb;padding:6px 8px;text-align:left;vertical-align:top}
th{background:#f0f0f0}code,pre{font-family:ui-monospace,Menlo,Consolas,monospace}td code{white-space:nowrap}
pre{background:#f4f4f4;border:1px solid #ccc;padding:8px 10px;overflow-x:auto}
.wrap{overflow-x:auto}
@media (prefers-color-scheme: dark){body{background:#121212;color:#e8e8e8}th{background:#262626}pre{background:#1d1d1d;border-color:#444}th,td{border-color:#444}}
</style></head><body>
<h1>{{.Name}}: network requirements</h1>
<p>Devices join {{.Name}} with the NetBird client. Before anyone at your site enrolls, your network must allow the <b>outbound</b> connections below. Many organisation networks allow the first row (ordinary HTTPS) but block the others.</p>
<h2>What to allow</h2>
{{if .OK}}<div class="wrap"><table>
<tr><th>Host name</th><th>Port / protocol</th><th>Purpose</th><th>Needed</th></tr>
{{range .Rows}}<tr><td><code>{{.Host}}</code></td><td>{{.Ports}}</td><td>{{.Purpose}}</td><td>{{.Needed}}</td></tr>
{{end}}<tr><td>Other {{.Name}} devices (any address)</td><td>UDP, high ports</td><td>Direct connections between devices; faster than relaying</td><td>Recommended</td></tr>
</table></div>
<p>Allow every host listed: a device opens connections to more than one relay.</p>
{{else}}<p>This network's relay list is not configured on this page. Ask the person who invited you for the host names.</p>{{end}}
<h2>Check before you start</h2>
<p>Run this from a computer on your site network. It must report the connection as succeeded or open.</p>
<pre>{{if .Test}}{{.Test}}{{else}}nc -zv &lt;relay host&gt; &lt;relay port&gt;{{end}}</pre>
<h2>If it is blocked</h2>
<p>{{.Symptom}} The device has registered, but it cannot reach any other device, so no {{.Name}} service is reachable from it. Run <code>netbird status -d</code>: if relays show <code>context deadline exceeded</code> and the peer count is 0, the site network is blocking the ports above.</p>
</body></html>
`))
