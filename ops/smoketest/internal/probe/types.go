// Package probe defines the core types and interfaces for the smoketest probe system.
package probe

import (
	"context"
	"time"
)

// Status represents the outcome of a single check.
type Status string

const (
	Pass Status = "PASS"
	Fail Status = "FAIL"
	Warn Status = "WARN"
	Skip Status = "SKIP"
)

// Category groups related checks.
type Category string

const (
	CatDNS        Category = "dns"
	CatRelay      Category = "relay"
	CatSTUN       Category = "stun"
	CatTLS        Category = "tls"
	CatMesh       Category = "mesh"
	CatService    Category = "service"
	CatLatency    Category = "latency"
	CatLoad       Category = "load"
)

// Result captures the outcome of a single check.
type Result struct {
	Category  Category      `json:"category"`
	Name      string        `json:"name"`
	Status    Status        `json:"status"`
	Detail    string        `json:"detail"`
	LatencyMs float64       `json:"latency_ms,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// RunResult is the aggregate output of a full probe run.
type RunResult struct {
	RunID     string    `json:"run_id"`
	Timestamp time.Time `json:"timestamp"`
	Mode      string    `json:"mode"`
	DurationS float64   `json:"duration_s"`
	Pass      int       `json:"pass"`
	Fail      int       `json:"fail"`
	Warn      int       `json:"warn"`
	Checks    []Result  `json:"checks"`
}

// Probe is the interface every probe module implements.
type Probe interface {
	// Category returns the probe's category.
	Category() Category
	// Run executes all checks in this probe and returns results.
	Run(ctx context.Context, cfg *Config) []Result
}

// RelayInfo describes a single VPS relay.
type RelayInfo struct {
	Name       string `yaml:"name" json:"name"`
	IP         string `yaml:"ip" json:"ip"`
	Region     string `yaml:"region" json:"region"`
	InstanceID string `yaml:"instance_id" json:"instance_id"`
}

// Config holds all configuration needed by probes.
type Config struct {
	Domain       string      `yaml:"domain" json:"domain"`
	Relays       []RelayInfo `yaml:"relays" json:"relays"`
	NETHOSTMeshIP   string      `yaml:"nethost_mesh_ip" json:"nethost_mesh_ip"`
	RelayDNS     []string    `yaml:"relay_dns" json:"relay_dns"`
	ExpectedIPs  []string    `yaml:"expected_ips" json:"expected_ips"`
	NetbirdToken string      `yaml:"-" json:"-"`
	Mode         string      `yaml:"-" json:"-"` // "quick" or "full"
	Categories   []Category  `yaml:"-" json:"-"` // filter to these categories (empty = all)
}

// DefaultConfig returns the standard goat configuration.
func DefaultConfig() *Config {
	return &Config{
		Domain:     "netbird.legacy.example",
		NETHOSTMeshIP: "100.127.1.34",
		Relays: []RelayInfo{
			{Name: "fra", IP: "203.0.113.2", Region: "eu-central-1", InstanceID: "i-00000cea114dd2d2f"},
			{Name: "isr", IP: "203.0.113.20", Region: "il-central-1", InstanceID: "i-00000c028101b56e6"},
			{Name: "mum", IP: "203.0.113.30", Region: "ap-south-1", InstanceID: "i-00000aeee9f4c9fcd"},
		},
		RelayDNS: []string{
			"relay.legacy.example",
			"relay-rlb.legacy.example",
			"relay-rlc.legacy.example",
		},
		ExpectedIPs: []string{"203.0.113.2", "203.0.113.20", "203.0.113.30"},
		Mode:        "quick",
	}
}
