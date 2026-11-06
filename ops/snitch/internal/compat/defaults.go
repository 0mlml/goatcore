package compat

// defaults.go: built-in defaults that name the running deployment. Each one
// applies only when its environment variable is unset.

// PeerFQDNSuffixes are stripped from a peer's NetBird FQDN to make its
// auto-alias. Override: SNITCH_PEER_DOMAIN_SUFFIXES (comma-separated).
var PeerFQDNSuffixes = []string{".netbird.legacy.example", ".local.netbird.legacy.example"}

// GatewatchURL is the gatewatch service the dashboard reads bridge status
// from. Override: SNITCH_GATEWATCH_URL.
const GatewatchURL = "https://gatewatch.netbird.legacy.example"

// InstallCollectorURL is the collector URL written into the served
// /install script when the installing host does not set SNITCH_COLLECTOR.
// Override: SNITCH_PUBLIC_URL.
const InstallCollectorURL = "https://snitch.netbird.legacy.example"

// SatlinkTarget is a relay used as a latency reference by the satellite-link
// PoP benchmark.
type SatlinkTarget struct {
	Name   string
	IP     string
	Region string
}

// SatlinkTargets are the relays the satellite-link PoP benchmark measures.
var SatlinkTargets = []SatlinkTarget{
	{Name: "fra", IP: "203.0.113.2", Region: "eu-central-1"},
	{Name: "isr", IP: "203.0.113.20", Region: "il-central-1"},
	{Name: "mum", IP: "203.0.113.30", Region: "ap-south-1"},
}
