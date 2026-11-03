// Zenoh load generator for Phase 2 soak test (Step 6).
// Publishes synthetic ops/loadgen/** events via zenoh REST PUT.
// Three scenarios: baseline (10/hr), persistent-10x (100/hr), burst (1000/hr × 10min).
//
// Usage:
//   ZENOH_ENDPOINT=http://100.x.x.x:8088 SCENARIO=baseline ./loadgen
//
// Env vars:
//   ZENOH_ENDPOINT  — required, e.g. http://100.x.x.x:8088
//   SCENARIO        — baseline | persistent10x | burst  (default: baseline)
//   BURST_DURATION  — duration of burst window, e.g. 10m  (default: 10m)
//   KEY_PREFIX      — key expression prefix  (default: ops/loadgen)
//   DRY_RUN         — set to "1" to print events without sending

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type event struct {
	Seq       int64     `json:"seq"`
	Scenario  string    `json:"scenario"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	Payload   string    `json:"payload"`
}

type scenario struct {
	name       string
	ratePerHr  int
	duration   time.Duration // 0 = run forever
}

var scenarios = map[string]scenario{
	"baseline":      {name: "baseline", ratePerHr: 10, duration: 0},
	"persistent10x": {name: "persistent10x", ratePerHr: 100, duration: 0},
	"burst":         {name: "burst", ratePerHr: 1000, duration: 10 * time.Minute},
}

func main() {
	endpoint := os.Getenv("ZENOH_ENDPOINT")
	if endpoint == "" {
		log.Fatal("ZENOH_ENDPOINT is required (e.g. http://100.x.x.x:8088)")
	}
	endpoint = strings.TrimRight(endpoint, "/")

	scenarioName := os.Getenv("SCENARIO")
	if scenarioName == "" {
		scenarioName = "baseline"
	}
	sc, ok := scenarios[scenarioName]
	if !ok {
		log.Fatalf("unknown scenario %q — valid: baseline, persistent10x, burst", scenarioName)
	}

	if bd := os.Getenv("BURST_DURATION"); bd != "" && scenarioName == "burst" {
		d, err := time.ParseDuration(bd)
		if err != nil {
			log.Fatalf("invalid BURST_DURATION %q: %v", bd, err)
		}
		sc.duration = d
	}

	keyPrefix := os.Getenv("KEY_PREFIX")
	if keyPrefix == "" {
		keyPrefix = "ops/loadgen"
	}
	keyPrefix = strings.TrimRight(keyPrefix, "/")

	dryRun := os.Getenv("DRY_RUN") == "1"

	interval := time.Duration(float64(time.Hour) / float64(sc.ratePerHr))
	log.Printf("loadgen starting: scenario=%s rate=%d/hr interval=%s dry_run=%v",
		sc.name, sc.ratePerHr, interval, dryRun)
	if sc.duration > 0 {
		log.Printf("burst window: %s", sc.duration)
	}

	var (
		seq     int64
		client  = &http.Client{Timeout: 3 * time.Second}
		start   = time.Now()
		ticker  = time.NewTicker(interval)
		hostID  = hostname()
	)
	defer ticker.Stop()

	for range ticker.C {
		if sc.duration > 0 && time.Since(start) >= sc.duration {
			log.Printf("burst window elapsed after %d events — exiting", seq)
			return
		}
		seq++
		ev := event{
			Seq:       seq,
			Scenario:  sc.name,
			Source:    hostID,
			Timestamp: time.Now().UTC(),
			Payload:   randomPayload(),
		}
		body, _ := json.Marshal(ev)

		// Spread across a small key fan-out to exercise cardinality handling.
		bucket := seq % 4
		keyexpr := fmt.Sprintf("%s/soak/%s/%d", keyPrefix, sc.name, bucket)

		if dryRun {
			log.Printf("[dry] PUT %s  seq=%d", keyexpr, seq)
			continue
		}

		url := endpoint + "/" + keyexpr
		req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
		if err != nil {
			log.Printf("build request error: %v", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("PUT %s failed (seq=%d): %v", keyexpr, seq, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			log.Printf("PUT %s unexpected status %d (seq=%d)", keyexpr, resp.StatusCode, seq)
		} else if seq%50 == 0 {
			log.Printf("PUT %s ok seq=%d elapsed=%s", keyexpr, seq, time.Since(start).Round(time.Second))
		}
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown-" + strconv.Itoa(rand.Intn(9999))
	}
	return h
}

func randomPayload() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	n := 32 + rand.Intn(64)
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}
