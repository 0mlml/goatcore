package probe

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Registry holds all registered probes.
var registry []Probe

// Register adds a probe to the global registry.
func Register(p Probe) {
	registry = append(registry, p)
}

// Runner orchestrates probe execution.
type Runner struct {
	cfg *Config
}

// NewRunner creates a runner with the given config.
func NewRunner(cfg *Config) *Runner {
	return &Runner{cfg: cfg}
}

// Run executes all registered probes (or filtered by category) and returns aggregate results.
func (r *Runner) Run(ctx context.Context) *RunResult {
	start := time.Now()
	runID := start.Format("20060102T150405")

	probes := r.filteredProbes()

	// Run probes concurrently, collecting results.
	var mu sync.Mutex
	var allResults []Result
	var wg sync.WaitGroup

	for _, p := range probes {
		wg.Add(1)
		go func(p Probe) {
			defer wg.Done()
			results := p.Run(ctx, r.cfg)
			mu.Lock()
			allResults = append(allResults, results...)
			mu.Unlock()
		}(p)
	}
	wg.Wait()

	duration := time.Since(start)

	pass, fail, warn := 0, 0, 0
	for _, res := range allResults {
		switch res.Status {
		case Pass:
			pass++
		case Fail:
			fail++
		case Warn:
			warn++
		}
	}

	return &RunResult{
		RunID:     runID,
		Timestamp: start.UTC(),
		Mode:      r.cfg.Mode,
		DurationS: duration.Seconds(),
		Pass:      pass,
		Fail:      fail,
		Warn:      warn,
		Checks:    allResults,
	}
}

// filteredProbes returns probes matching the configured categories.
func (r *Runner) filteredProbes() []Probe {
	if len(r.cfg.Categories) == 0 {
		return registry
	}
	allowed := make(map[Category]bool)
	for _, c := range r.cfg.Categories {
		allowed[c] = true
	}
	var filtered []Probe
	for _, p := range registry {
		if allowed[p.Category()] {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// PrintTerminal prints results in a colored terminal format.
func PrintTerminal(rr *RunResult) {
	fmt.Println()
	fmt.Println("===============================================================================")
	fmt.Printf("  goat Smoke Test — %s\n", rr.RunID)
	fmt.Printf("  Mode: %s | Duration: %.1fs\n", rr.Mode, rr.DurationS)
	fmt.Println("===============================================================================")

	currentCat := Category("")
	for _, r := range rr.Checks {
		if r.Category != currentCat {
			fmt.Println()
			fmt.Printf("  [%s]\n", r.Category)
			fmt.Printf("  %-34s %-6s %s\n", "CHECK", "STATUS", "DETAIL")
			fmt.Printf("  %-34s %-6s %s\n", "-----", "------", "------")
			currentCat = r.Category
		}
		color, reset := statusColor(r.Status)
		latStr := ""
		if r.LatencyMs > 0 {
			latStr = fmt.Sprintf(" (%.0fms)", r.LatencyMs)
		}
		fmt.Printf("  %-34s %s%-6s%s %s%s\n", r.Name, color, r.Status, reset, r.Detail, latStr)
	}

	fmt.Println()
	fmt.Println("  ─────────────────────────────────────────────────────────────────")
	fmt.Printf("  PASS: %d  FAIL: %d  WARN: %d  TOTAL: %d\n", rr.Pass, rr.Fail, rr.Warn, rr.Pass+rr.Fail+rr.Warn)
	fmt.Println("  ─────────────────────────────────────────────────────────────────")
	fmt.Println()
}

func statusColor(s Status) (string, string) {
	switch s {
	case Pass:
		return "\033[32m", "\033[0m"
	case Fail:
		return "\033[31m", "\033[0m"
	case Warn:
		return "\033[33m", "\033[0m"
	default:
		return "", ""
	}
}
