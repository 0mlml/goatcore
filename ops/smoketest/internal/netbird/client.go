// Package netbird provides a client for the Netbird management API.
package netbird

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Peer represents a Netbird peer as returned by the management API.
type Peer struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	IP        string    `json:"ip"`
	Connected bool      `json:"connected"`
	LastSeen  time.Time `json:"last_seen"`
	OS        string    `json:"os"`
	Version   string    `json:"version"`
	Groups    []Group   `json:"groups"`
}

// Group is a Netbird group reference on a peer.
type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Client talks to the Netbird management HTTP API.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient creates a Netbird API client.
// domain is the management server hostname (e.g. "netbird.legacy.example").
// token is the API bearer token.
//
// The server certificate is verified against the system roots. Every request
// carries the management API token, so an unverified handshake would hand
// that token to anyone on the path. The load and relay probes already reach
// the same domain with verification on.
func NewClient(domain, token string) *Client {
	return &Client{
		baseURL: "https://" + domain,
		token:   token,
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			},
		},
	}
}

// ListPeers returns all peers from GET /api/peers.
func (c *Client) ListPeers(ctx context.Context) ([]Peer, error) {
	body, err := c.get(ctx, "/api/peers")
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	defer body.Close()

	var peers []Peer
	if err := json.NewDecoder(body).Decode(&peers); err != nil {
		return nil, fmt.Errorf("list peers: decode: %w", err)
	}
	return peers, nil
}

// GetAccounts calls GET /api/accounts as a health check.
// Returns the raw decoded JSON (typically a list of account objects).
func (c *Client) GetAccounts(ctx context.Context) (any, error) {
	body, err := c.get(ctx, "/api/accounts")
	if err != nil {
		return nil, fmt.Errorf("get accounts: %w", err)
	}
	defer body.Close()

	var result any
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get accounts: decode: %w", err)
	}
	return result, nil
}

// get performs an authenticated GET request and returns the response body.
// The caller must close the returned ReadCloser.
func (c *Client) get(ctx context.Context, path string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(snippet))
	}
	return resp.Body, nil
}
