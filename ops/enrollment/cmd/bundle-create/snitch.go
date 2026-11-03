package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	snitchProbeKeyBytes = 32
	snitchKeyPrefix     = "snitch_"
)

// generateSnitchProbeKey returns 32 random bytes that the device-side
// snitch-bundle role will hex-encode + prefix with "snitch_" to form the
// bearer token sent to the collector. The collector hashes that prefixed
// hex string and compares to the stored hash. Bundle-create stays
// agnostic to the prefix; the env-template renders it.
func generateSnitchProbeKey() ([]byte, error) {
	buf := make([]byte, snitchProbeKeyBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("rand read: %w", err)
	}
	return buf, nil
}

// snitchBearerString returns the wire form of the probe key as the
// device + collector both compute it: "snitch_" + hex(raw). This is
// the plaintext that is sent to the collector for registration so
// the collector hashes the SAME string the probe will later present.
func snitchBearerString(raw []byte) string {
	return snitchKeyPrefix + hex.EncodeToString(raw)
}

// registerSnitchKey POSTs the per-device probe key to the snitch
// collector's /api/keys endpoint with the supplied admin key, so a
// freshly-onboarded peer's probe is admitted on first contact. The
// collector stores SHA-256(plaintext); we send the same plaintext the
// device's env file will end up containing.
//
// Idempotent on label: re-issuing for the same device-id replaces the
// prior key (handled server-side by CreateKeyWithPlaintext).
func registerSnitchKey(ctx context.Context, collectorURL, adminKey, deviceID string, probeKey []byte) error {
	if collectorURL == "" {
		return fmt.Errorf("collector URL empty")
	}
	if adminKey == "" {
		return fmt.Errorf("admin key empty")
	}
	endpoint := strings.TrimRight(collectorURL, "/") + "/api/keys"

	body, err := json.Marshal(map[string]string{
		"role":          "probe",
		"label":         deviceID,
		"plaintext_key": snitchBearerString(probeKey),
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return fmt.Errorf("collector returned %d and its response could not be read: %w", resp.StatusCode, readErr)
		}
		return fmt.Errorf("collector returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}
