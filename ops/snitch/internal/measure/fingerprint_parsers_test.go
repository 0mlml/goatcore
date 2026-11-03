// Block 61H — cross-platform tests for the per-OS fingerprint parsers.
// All canned outputs verified against real `route` / `ip` / `Get-NetRoute`
// / `networksetup` / `iwgetid` / `netsh wlan` invocations on representative
// hosts during the multi-platform-builds rollout.

package measure

import "testing"

func TestParseDefaultRouteIface_Linux(t *testing.T) {
	out := `default via 192.168.1.1 dev eth0 proto dhcp metric 100
`
	if got := parseDefaultRouteIface("linux", out); got != "eth0" {
		t.Errorf("Linux: got %q, want eth0", got)
	}
}

func TestParseDefaultRouteIface_LinuxWlan(t *testing.T) {
	out := `default via 10.0.0.1 dev wlan0 proto dhcp src 10.0.0.5 metric 600
`
	if got := parseDefaultRouteIface("linux", out); got != "wlan0" {
		t.Errorf("Linux wlan: got %q, want wlan0", got)
	}
}

func TestParseDefaultRouteIface_Darwin(t *testing.T) {
	out := `   route to: default
destination: default
       mask: default
    gateway: 192.168.1.1
  interface: en0
      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,GLOBAL>
`
	if got := parseDefaultRouteIface("darwin", out); got != "en0" {
		t.Errorf("Darwin: got %q, want en0", got)
	}
}

func TestParseDefaultRouteIface_Windows_Format(t *testing.T) {
	// Format-List output: each property on its own line, "Name : Value".
	out := "\r\nInterfaceAlias : Ethernet 2\r\nNextHop        : 192.168.1.1\r\nInterfaceIndex : 12\r\n"
	if got := parseDefaultRouteIface("windows", out); got != "Ethernet 2" {
		t.Errorf("Windows: got %q, want 'Ethernet 2'", got)
	}
}

func TestParseDefaultRouteIface_Windows_WiFi(t *testing.T) {
	out := "InterfaceAlias : Wi-Fi\r\nNextHop        : 192.168.1.1\r\n"
	if got := parseDefaultRouteIface("windows", out); got != "Wi-Fi" {
		t.Errorf("Windows wifi: got %q, want Wi-Fi", got)
	}
}

func TestParseDefaultRouteIface_Empty(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		if got := parseDefaultRouteIface(goos, ""); got != "" {
			t.Errorf("%s empty: got %q, want \"\"", goos, got)
		}
	}
}

func TestParseDefaultGatewayIP_Linux(t *testing.T) {
	out := `default via 192.168.1.1 dev eth0 proto dhcp metric 100
`
	if got := parseDefaultGatewayIP("linux", out); got != "192.168.1.1" {
		t.Errorf("Linux: got %q, want 192.168.1.1", got)
	}
}

func TestParseDefaultGatewayIP_Darwin(t *testing.T) {
	out := `   route to: default
destination: default
    gateway: 192.168.1.1
  interface: en0
`
	if got := parseDefaultGatewayIP("darwin", out); got != "192.168.1.1" {
		t.Errorf("Darwin: got %q, want 192.168.1.1", got)
	}
}

func TestParseDefaultGatewayIP_Windows(t *testing.T) {
	out := "NextHop        : 192.168.1.1\r\nInterfaceAlias : Ethernet 2\r\n"
	if got := parseDefaultGatewayIP("windows", out); got != "192.168.1.1" {
		t.Errorf("Windows: got %q, want 192.168.1.1", got)
	}
}

func TestParseSSID_Darwin(t *testing.T) {
	out := "Current Wi-Fi Network: MyHomeNetwork\n"
	if got := parseSSID("darwin", out); got != "MyHomeNetwork" {
		t.Errorf("Darwin: got %q, want MyHomeNetwork", got)
	}
}

func TestParseSSID_Darwin_NotAssociated(t *testing.T) {
	out := "You are not associated with an AirPort network."
	if got := parseSSID("darwin", out); got != "" {
		t.Errorf("Darwin not-associated: got %q, want \"\"", got)
	}
}

func TestParseSSID_Linux(t *testing.T) {
	// iwgetid -r returns just the SSID.
	out := "MyHomeNetwork\n"
	if got := parseSSID("linux", out); got != "MyHomeNetwork" {
		t.Errorf("Linux: got %q, want MyHomeNetwork", got)
	}
}

func TestParseSSID_Windows(t *testing.T) {
	// netsh wlan show interfaces. Real-world output has SSID + BSSID
	// lines and we must not return BSSID.
	out := "" +
		"There is 1 interface on the system:\r\n" +
		"\r\n" +
		"    Name                   : Wi-Fi\r\n" +
		"    SSID                   : MyHomeNetwork\r\n" +
		"    BSSID                  : 11:22:33:44:55:66\r\n" + // opsec-allow: synthetic test MAC
		"    Network type           : Infrastructure\r\n"
	if got := parseSSID("windows", out); got != "MyHomeNetwork" {
		t.Errorf("Windows: got %q, want MyHomeNetwork", got)
	}
}

func TestParseSSID_Windows_BSSIDOnlyDoesNotFalseMatch(t *testing.T) {
	// If only a BSSID line is present (e.g. mid-roaming), we should not
	// return the MAC as if it were the SSID.
	out := "    BSSID                  : 11:22:33:44:55:66\r\n" // opsec-allow: synthetic test MAC
	if got := parseSSID("windows", out); got != "" {
		t.Errorf("Windows BSSID-only: got %q, want \"\"", got)
	}
}

func TestParseMACFromARP_LinuxIPNeigh(t *testing.T) {
	// `ip neigh show 192.168.1.1`:
	out := "192.168.1.1 dev eth0 lladdr aa:bb:cc:dd:ee:ff REACHABLE\n" // opsec-allow: synthetic test MAC
	if got := parseMACFromARP(out); got != "aa:bb:cc:dd:ee:ff" {       // opsec-allow: synthetic test MAC
		t.Errorf("Linux ip neigh: got %q, want aa:bb:cc:dd:ee:ff", got) // opsec-allow: synthetic test MAC
	}
}

func TestParseMACFromARP_DarwinARPN(t *testing.T) {
	// `arp -n 192.168.1.1`:
	out := "? (192.168.1.1) at 74:24:9f:00:00:01 on en0 ifscope [ethernet]\n" // opsec-allow: synthetic test MAC
	if got := parseMACFromARP(out); got != "74:24:9f:00:00:01" {              // opsec-allow: synthetic test MAC
		t.Errorf("Darwin arp -n: got %q, want 74:24:9f:00:00:01", got) // opsec-allow: synthetic test MAC
	}
}

func TestParseMACFromARP_WindowsARPA(t *testing.T) {
	// `arp -a 192.168.1.1`:
	out := "" +
		"\r\n" +
		"Interface: 192.168.1.10 --- 0xc\r\n" +
		"  Internet Address      Physical Address      Type\r\n" +
		"  192.168.1.1           74-24-9f-00-00-01     dynamic\r\n" // opsec-allow: synthetic test MAC
	if got := parseMACFromARP(out); got != "74:24:9f:00:00:01" { // opsec-allow: synthetic test MAC
		t.Errorf("Windows arp -a: got %q, want 74:24:9f:00:00:01 (dash→colon norm)", got) // opsec-allow: synthetic test MAC
	}
}

func TestParseMACFromARP_NoMatch(t *testing.T) {
	out := "no entry found"
	if got := parseMACFromARP(out); got != "" {
		t.Errorf("no-match: got %q, want \"\"", got)
	}
}

func TestNormalizeMac_ColonZeroPad(t *testing.T) {
	if got := normalizeMac("0:1:A:b:cc:dd"); got != "00:01:0a:0b:cc:dd" { // opsec-allow: synthetic test MAC
		t.Errorf("zero-pad: got %q", got)
	}
}

func TestNormalizeMac_DashSeparator(t *testing.T) {
	if got := normalizeMac("AA-BB-CC-DD-EE-FF"); got != "aa:bb:cc:dd:ee:ff" { // opsec-allow: synthetic test MAC
		t.Errorf("dash separator: got %q", got)
	}
}

func TestNormalizeMac_AlreadyNormal(t *testing.T) {
	if got := normalizeMac("aa:bb:cc:dd:ee:ff"); got != "aa:bb:cc:dd:ee:ff" { // opsec-allow: synthetic test MAC
		t.Errorf("already normal: got %q", got)
	}
}

// ouiLookup correctness tests — the OUI table is load-bearing for
// distinguishing Starlink / commercial / partner gear in fleet
// dashboards. Spot-check a couple of known prefixes.
func TestOUILookup_Starlink(t *testing.T) {
	if got := ouiLookup("74:24:9f:00:00:01"); got != "SpaceX Starlink" { // opsec-allow: synthetic test MAC
		t.Errorf("Starlink: got %q", got)
	}
}

func TestOUILookup_Apple(t *testing.T) {
	if got := ouiLookup("3c:22:fb:11:22:33"); got != "Apple" { // opsec-allow: synthetic test MAC
		t.Errorf("Apple: got %q", got)
	}
}

func TestOUILookup_Unknown(t *testing.T) {
	if got := ouiLookup("ff:ff:ff:00:00:00"); got != "" { // opsec-allow: synthetic test MAC
		t.Errorf("unknown: got %q", got)
	}
}

func TestOUILookup_TooShort(t *testing.T) {
	if got := ouiLookup("aa:bb"); got != "" {
		t.Errorf("too-short: got %q", got)
	}
}

func TestClassifyInterface_Darwin(t *testing.T) {
	// Darwin en0 is not classifiable without isWiFiInterface — but
	// classifyInterface itself returns "ethernet" when isWiFiInterface
	// returns false (which it does on non-Darwin GOOS). Verify the
	// fallback path via Linux-style names.
	cases := map[string]string{
		"eth0":    "ethernet",
		"wlan0":   "wifi",
		"wlp3s0":  "wifi",
		"wwan0":   "cellular",
		"rmnet0":  "cellular",
		"utun5":   "tunnel",
		"wg0":     "tunnel",
		"strange": "unknown",
	}
	for name, want := range cases {
		if got := classifyInterface(name); got != want {
			t.Errorf("classifyInterface(%q) = %q, want %q", name, got, want)
		}
	}
}
