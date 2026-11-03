package welcome

// What a device on this goatnet dials outbound, rendered for someone whose
// network team has to write firewall rules before the device can join.
//
// The facts are this goatnet's own config — the management URL, the relay list
// handed to clients, and the STUN hosts — so the page cannot describe a relay
// the goatnet does not run. Addresses are resolved at request time by the
// handler (they live in DNS, not in config), and the page says when a name did
// not resolve to a public address rather than printing a guess.

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// EgressHost is one hostname a device dials, with the ports it uses there and
// the public addresses the hostname resolved to.
type EgressHost struct {
	Host  string
	Ports []string // e.g. "TCP 443", "UDP 3478" — sorted, de-duplicated
	// IPs are the PUBLIC addresses the name resolved to. Private, CGNAT and
	// loopback answers are dropped: a firewall on the reader's side can do
	// nothing with them, and they appear when the console resolves through
	// a local override rather than public DNS.
	IPs []string
	// Unresolved is set when the lookup failed or returned no public address.
	Unresolved bool
}

// EgressHostsFrom merges the three config facts into one row per hostname.
// relayAddrs are NetBird relay URLs (rels://host:port or rel://host:port);
// stunHosts are bare hostnames or host:port (port defaults to 3478).
func EgressHostsFrom(mgmtURL string, relayAddrs, stunHosts []string) []EgressHost {
	ports := map[string]map[string]bool{}
	var order []string
	add := func(host, port string) {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" {
			return
		}
		if ports[host] == nil {
			ports[host] = map[string]bool{}
			order = append(order, host)
		}
		ports[host][port] = true
	}

	if u, err := url.Parse(strings.TrimSpace(mgmtURL)); err == nil && u.Hostname() != "" {
		p := u.Port()
		if p == "" {
			p = "443"
		}
		add(u.Hostname(), "TCP "+p)
	}
	for _, r := range relayAddrs {
		u, err := url.Parse(strings.TrimSpace(r))
		if err != nil || u.Hostname() == "" {
			continue
		}
		p := u.Port()
		if p == "" {
			p = "443"
		}
		// The relay client races WebSocket (TCP) and QUIC (UDP) on the same
		// port and keeps whichever connects first.
		add(u.Hostname(), "TCP "+p)
		add(u.Hostname(), "UDP "+p)
	}
	for _, s := range stunHosts {
		s = strings.TrimSpace(s)
		s = strings.TrimPrefix(strings.TrimPrefix(s, "stun:"), "stuns:")
		host, port, err := net.SplitHostPort(s)
		if err != nil {
			host, port = s, "3478"
		}
		add(host, "UDP "+port)
	}

	out := make([]EgressHost, 0, len(order))
	for _, h := range order {
		var ps []string
		for p := range ports[h] {
			ps = append(ps, p)
		}
		sort.Slice(ps, func(i, j int) bool { return portLess(ps[i], ps[j]) })
		out = append(out, EgressHost{Host: h, Ports: ps})
	}
	return out
}

// portLess orders "TCP 443" before "TCP 33073" before "UDP 3478": protocol
// first, then port number.
func portLess(a, b string) bool {
	pa, na := splitPort(a)
	pb, nb := splitPort(b)
	if pa != pb {
		return pa < pb
	}
	return na < nb
}

func splitPort(s string) (string, int) {
	f := strings.Fields(s)
	if len(f) != 2 {
		return s, 0
	}
	n, _ := strconv.Atoi(f[1]) //nolint:errcheck // the matching expression admits decimal digits only
	return f[0], n
}

// PublicIPs keeps only addresses a remote firewall rule can use.
func PublicIPs(addrs []string) []string {
	cgnat := netip.MustParsePrefix("100.64.0.0/10")
	seen := map[string]bool{}
	var out []string
	for _, a := range addrs {
		ip, err := netip.ParseAddr(strings.TrimSpace(a))
		if err != nil || !ip.Is4() && !ip.Is6() {
			continue
		}
		ip = ip.Unmap()
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || cgnat.Contains(ip) {
			continue
		}
		if s := ip.String(); !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// egressSections renders the per-goatnet part of the firewall topic: a table
// of hostnames, and the de-duplicated address list a firewall rule needs.
func egressSections(cat Catalog) []Section {
	if len(cat.Egress) == 0 {
		return []Section{{
			ID:      "egress-addresses",
			Heading: "This goatnet's addresses",
			Paras: []string{
				"This goatnet's guide was not configured with its relay list, so it cannot print the addresses here. " +
					"The goatnet's operators can supply them; the hostnames are the management server in your setup instructions and the relays that server hands out.",
			},
		}}
	}

	w := len("Hostname")
	for _, h := range cat.Egress {
		if len(h.Host) > w {
			w = len(h.Host)
		}
	}
	var tbl strings.Builder
	var unresolved []string
	var all []string
	fmt.Fprintf(&tbl, "%-*s  %s\n", w, "Hostname", "Ports, then addresses")
	for _, h := range cat.Egress {
		fmt.Fprintf(&tbl, "%-*s  %s\n", w, h.Host, strings.Join(h.Ports, ", "))
		if h.Unresolved {
			fmt.Fprintf(&tbl, "%-*s  %s\n", w, "", "(did not resolve to a public address from here)")
			unresolved = append(unresolved, h.Host)
		}
		// Two addresses per line: a round-robin name carries several, and one
		// long line runs off the page when the guide is printed to hand over.
		for i := 0; i < len(h.IPs); i += 2 {
			fmt.Fprintf(&tbl, "%-*s  %s\n", w, "", strings.Join(h.IPs[i:min(i+2, len(h.IPs))], "  "))
		}
		all = append(all, h.IPs...)
	}

	paras := []string{
		"Every connection a device makes to join and use " + DisplayName(cat.Goatnet) + " goes to one of these hostnames. " +
			"Allow ALL of them: a device keeps one relay as its home but opens a connection to another relay whenever the peer it wants is homed there, so a partial list breaks some connections and not others.",
	}
	if len(unresolved) > 0 {
		paras = append(paras, "Some names did not resolve to a public address when this page was built. Run `dig +short <hostname>` from outside the goatnet for those before writing the rule.")
	}

	secs := []Section{{
		ID:      "egress-addresses",
		Heading: "This goatnet's addresses",
		Paras:   paras,
		Pre:     strings.TrimRight(tbl.String(), "\n"),
	}}

	if ips := PublicIPs(all); len(ips) > 0 {
		var rule strings.Builder
		for _, ip := range ips {
			if strings.Contains(ip, ":") {
				rule.WriteString(ip + "/128\n")
			} else {
				rule.WriteString(ip + "/32\n")
			}
		}
		secs = append(secs, Section{
			ID:      "egress-rule",
			Heading: "The address list, ready for a rule",
			Paras: []string{
				"The same addresses, de-duplicated, one per line. The management name resolves round-robin across the relays on most goatnets, so this list is usually shorter than the table.",
			},
			Pre: strings.TrimRight(rule.String(), "\n"),
		})
	}
	return secs
}
