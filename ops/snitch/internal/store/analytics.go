package store

import (
	"context"
	"fmt"
	"time"
)

// TimePoint is a single bucketed data point in a time-series.
type TimePoint struct {
	Timestamp  int64   `json:"ts"`
	LatencyAvg float64 `json:"latency_avg"`
	LatencyP95 float64 `json:"latency_p95"`
	Jitter     float64 `json:"jitter"`
	Loss       float64 `json:"loss"`
	DirectPct  float64 `json:"direct_pct"`
	Samples    int     `json:"samples"`
}

// EventRecord is a state change event with optional peer name resolution.
type EventRecord struct {
	Timestamp   int64  `json:"ts"`
	Token       string `json:"token"`
	PeerName    string `json:"peer_name,omitempty"`
	EventType   string `json:"event_type"`
	TargetToken string `json:"target_token,omitempty"`
	TargetName  string `json:"target_name,omitempty"`
	OldValue    string `json:"old_value,omitempty"`
	NewValue    string `json:"new_value,omitempty"`
	Details     string `json:"details,omitempty"`
}

// UptimeRecord is uptime for a single peer over a time range.
type UptimeRecord struct {
	Token       string  `json:"token"`
	PeerName    string  `json:"peer_name,omitempty"`
	UptimePct   float64 `json:"uptime_pct"`
	ReportCount int     `json:"report_count"`
}

// ParseRange converts a range string (1h, 6h, 24h, 7d, 30d) to absolute
// unix timestamps and an appropriate bucket size in seconds.
func ParseRange(r string) (from, to, bucket int64, err error) {
	now := time.Now().Unix()
	switch r {
	case "1h":
		return now - 3600, now, 60, nil
	case "6h":
		return now - 6*3600, now, 120, nil
	case "24h":
		return now - 24*3600, now, 300, nil
	case "7d":
		return now - 7*24*3600, now, 1800, nil
	case "30d":
		return now - 30*24*3600, now, 7200, nil
	default:
		return 0, 0, 0, fmt.Errorf("unknown range %q (valid: 1h, 6h, 24h, 7d, 30d)", r)
	}
}

// MeshTrend returns bucketed mesh-wide latency/loss averages over time.
func (s *Store) MeshTrend(ctx context.Context, from, to, bucket int64) ([]TimePoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT (ts / $1) * $1 AS bucket_ts,
			AVG(latency_avg), AVG(latency_p95), AVG(jitter), AVG(loss),
			AVG(CASE WHEN direct THEN 100.0 ELSE 0.0 END),
			COUNT(*)
		FROM measurements
		WHERE ts >= $2 AND ts <= $3 AND connected = TRUE
		GROUP BY bucket_ts
		ORDER BY bucket_ts`,
		bucket, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var points []TimePoint
	for rows.Next() {
		var p TimePoint
		if err := rows.Scan(&p.Timestamp, &p.LatencyAvg, &p.LatencyP95, &p.Jitter,
			&p.Loss, &p.DirectPct, &p.Samples); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// LinkTrend returns bucketed time-series for a specific from->to link.
func (s *Store) LinkTrend(ctx context.Context, fromToken, toToken string, from, to, bucket int64) ([]TimePoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT (ts / $1) * $1 AS bucket_ts,
			AVG(latency_avg), AVG(latency_p95), AVG(jitter), AVG(loss),
			AVG(CASE WHEN direct THEN 100.0 ELSE 0.0 END),
			COUNT(*)
		FROM measurements
		WHERE from_token = $2 AND to_token = $3 AND ts >= $4 AND ts <= $5 AND connected = TRUE
		GROUP BY bucket_ts
		ORDER BY bucket_ts`,
		bucket, fromToken, toToken, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var points []TimePoint
	for rows.Next() {
		var p TimePoint
		if err := rows.Scan(&p.Timestamp, &p.LatencyAvg, &p.LatencyP95, &p.Jitter,
			&p.Loss, &p.DirectPct, &p.Samples); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// RecentEvents returns the most recent events in a time range.
func (s *Store) RecentEvents(ctx context.Context, from, to int64, limit int) ([]EventRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ts, token, event_type, COALESCE(target_token,''),
			COALESCE(old_value,''), COALESCE(new_value,''), COALESCE(details,'')
		FROM events
		WHERE ts >= $1 AND ts <= $2
		ORDER BY ts DESC
		LIMIT $3`,
		from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []EventRecord
	for rows.Next() {
		var e EventRecord
		if err := rows.Scan(&e.Timestamp, &e.Token, &e.EventType, &e.TargetToken,
			&e.OldValue, &e.NewValue, &e.Details); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// PeerUptime returns uptime percentage for each peer in a time range.
func (s *Store) PeerUptime(ctx context.Context, from, to int64) ([]UptimeRecord, error) {
	totalMinutes := (to - from) / 60
	if totalMinutes < 1 {
		totalMinutes = 1
	}
	rows, err := s.pool.Query(ctx, `
		SELECT token, COUNT(DISTINCT ts / 60) AS report_minutes
		FROM reports
		WHERE ts >= $1 AND ts <= $2
		GROUP BY token`,
		from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []UptimeRecord
	for rows.Next() {
		var r UptimeRecord
		if err := rows.Scan(&r.Token, &r.ReportCount); err != nil {
			return nil, err
		}
		r.UptimePct = float64(r.ReportCount) / float64(totalMinutes) * 100
		if r.UptimePct > 100 {
			r.UptimePct = 100
		}
		records = append(records, r)
	}
	return records, rows.Err()
}
