// Block 61H — pure-function parsers for the per-OS network-fingerprint
// probes. Same rationale as relay_runner_parsers.go: text-shaping logic
// out of the per-OS exec callsites so it runs as a unit test on any
// developer host.

package measure

import (
	"regexp"
	"strings"
)

// parseDefaultRouteIface extracts the default-route interface name from
// per-OS `route` / `ip` / `Get-NetRoute` output. Pure function over
// (goos, output). Returns "" if not found.
//
// Block 61H consolidated all three platforms here so a Mac developer
// can verify the Windows + Linux parsers without spinning up VMs.
func parseDefaultRouteIface(goos, out string) string {
	switch goos {
	case "darwin":
		// `route -n get default`:
		//    interface: en0
		re := regexp.MustCompile(`(?m)^\s*interface:\s*(\S+)\s*$`)
		if m := re.FindStringSubmatch(out); len(m) > 1 {
			return m[1]
		}
	case "windows":
		// PowerShell `Format-List InterfaceAlias,...`:
		//   InterfaceAlias : Ethernet 2
		re := regexp.MustCompile(`(?m)^InterfaceAlias\s*:\s*(.+?)\s*$`)
		if m := re.FindStringSubmatch(out); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	default:
		// Linux iproute2 `ip route show default`:
		//   default via 192.168.1.1 dev eth0 proto dhcp metric 100
		re := regexp.MustCompile(`dev\s+(\S+)`)
		if m := re.FindStringSubmatch(out); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// parseDefaultGatewayIP extracts the default-route next-hop IP from
// per-OS output. Pure function over (goos, output). Returns "" if not
// found.
func parseDefaultGatewayIP(goos, out string) string {
	switch goos {
	case "darwin":
		// `route -n get default`:
		//    gateway: 192.168.1.1
		re := regexp.MustCompile(`(?m)^\s*gateway:\s*(\S+)\s*$`)
		if m := re.FindStringSubmatch(out); len(m) > 1 {
			return m[1]
		}
	case "windows":
		// PowerShell `Format-List NextHop`:
		//   NextHop : 192.168.1.1
		re := regexp.MustCompile(`(?m)^NextHop\s*:\s*(\S+)\s*$`)
		if m := re.FindStringSubmatch(out); len(m) > 1 {
			return m[1]
		}
	default:
		// Linux iproute2:
		//   default via 192.168.1.1 dev eth0 ...
		re := regexp.MustCompile(`via\s+(\S+)`)
		if m := re.FindStringSubmatch(out); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// parseSSID extracts the connected-WiFi SSID from per-OS output. Pure
// function over (goos, output).
//
// Skips Windows BSSID lines (which also contain "BSSID :" → SSID-like
// shape but the value is xx:xx:xx:xx:xx:xx). The current heuristic is
// "first SSID line whose value does not look like a MAC."
func parseSSID(goos, out string) string {
	out = strings.TrimSpace(out)
	switch goos {
	case "darwin":
		// `networksetup -getairportnetwork en0`:
		//   Current Wi-Fi Network: MyNetwork
		// (Or "You are not associated with an AirPort network.")
		if strings.Contains(out, "not associated") {
			return ""
		}
		if idx := strings.Index(out, ": "); idx >= 0 {
			return strings.TrimSpace(out[idx+2:])
		}
	case "windows":
		// `netsh wlan show interfaces`:
		//     SSID                   : MyNetwork
		//     BSSID                  : 11:22:33:44:55:66 // opsec-allow: synthetic example
		// We want the first SSID line whose value isn't MAC-shaped.
		re := regexp.MustCompile(`(?m)^\s*SSID\s+:\s*(.+?)\s*$`)
		for _, m := range re.FindAllStringSubmatch(out, -1) {
			candidate := strings.TrimSpace(m[1])
			if !strings.Contains(candidate, ":") {
				return candidate
			}
		}
	default:
		// Linux `iwgetid -r eth0` returns just the SSID (no "key: value"
		// shape).
		return out
	}
	return ""
}

// parseMACFromARP extracts a MAC address from arbitrary per-OS arp
// output. Pure function. Tolerates both colon-separated (Linux/Darwin)
// and dash-separated (Windows) MAC formats; output is always
// lowercase + colon-separated + zero-padded.
func parseMACFromARP(out string) string {
	re := regexp.MustCompile(`([0-9a-fA-F]{1,2}[:\-][0-9a-fA-F]{1,2}[:\-][0-9a-fA-F]{1,2}[:\-][0-9a-fA-F]{1,2}[:\-][0-9a-fA-F]{1,2}[:\-][0-9a-fA-F]{1,2})`)
	if m := re.FindString(out); m != "" {
		return normalizeMac(m)
	}
	return ""
}
