package state

import "testing"

func TestNtpClassString(t *testing.T) {
	cases := []struct {
		c    NtpClass
		want string
	}{
		{ClassHealthy, "healthy"},
		{ClassWarning, "warning"},
		{ClassDegraded, "degraded"},
		{ClassUnknown, "unknown"},
		{NtpClass(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.c.String(); got != tc.want {
			t.Errorf("NtpClass(%d).String() = %q, want %q", tc.c, got, tc.want)
		}
	}
}

func TestClassify(t *testing.T) {
	// Table covers the universal-ntp-hygiene.md §4.3 classification rules.
	// rmsOffsetSec is in seconds; Classify converts to nanoseconds internally.
	cases := []struct {
		name         string
		stratum      uint16
		leapStatus   uint16
		rmsOffsetSec float64
		want         NtpClass
	}{
		{"unsynced via leapStatus=3", 2, 3, 0, ClassUnknown},
		{"unreachable stratum 16", 16, 0, 0, ClassUnknown},
		{"unreachable stratum 17", 17, 0, 0, ClassUnknown},
		{"orphan stratum 10", 10, 0, 0, ClassDegraded},
		{"orphan stratum 12", 12, 0, 0, ClassDegraded},
		{"healthy: stratum 1, sub-millisecond", 1, 0, 0.0001, ClassHealthy},
		{"healthy: stratum 4, exactly 1ms", 4, 0, 0.001, ClassHealthy},
		{"warning: stratum 4, just over 1ms", 4, 0, 0.0011, ClassWarning},
		{"warning: stratum 5, 5ms", 5, 0, 0.005, ClassWarning},
		{"warning: stratum 6, 10ms boundary", 6, 0, 0.01, ClassWarning},
		{"degraded: stratum 4, 50ms (over warning ceiling)", 4, 0, 0.05, ClassDegraded},
		{"degraded: stratum 7 forces drop from warning band", 7, 0, 0.005, ClassDegraded},
		{"degraded: 100ms boundary", 4, 0, 0.1, ClassDegraded},
		{"unknown: 200ms blows past degraded ceiling", 4, 0, 0.2, ClassUnknown},
		{"negative offset symmetric to positive", 4, 0, -0.0005, ClassHealthy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.stratum, tc.leapStatus, tc.rmsOffsetSec)
			if got != tc.want {
				t.Errorf("Classify(stratum=%d, leap=%d, rms=%g) = %s, want %s",
					tc.stratum, tc.leapStatus, tc.rmsOffsetSec, got, tc.want)
			}
		})
	}
}

func TestClassifySource(t *testing.T) {
	cases := []struct {
		refID   string
		stratum uint16
		want    SelectedSource
	}{
		{"GPS", 1, SourceGNSS},
		{"PPS", 1, SourceGNSS},
		{"GNSS", 1, SourceGNSS},
		{"GAL", 1, SourceGNSS},
		{"GLO", 1, SourceGNSS},
		{"BDS", 1, SourceGNSS},
		{"GPSDO", 1, SourceGNSS},
		{"TSIP", 1, SourceGNSS},
		{"time.cloudflare.com", 2, SourceWanNTS},
		{"any-non-gnss-refid", 3, SourceWanNTS},
		{"GPS", 10, SourceOrphan}, // stratum forces orphan even with GNSS-shaped refID
		{"GPS", 16, SourceNone},   // stratum 16 forces None even with GNSS-shaped refID
	}
	for _, tc := range cases {
		t.Run(tc.refID+"/"+itoa(tc.stratum), func(t *testing.T) {
			got := ClassifySource(tc.refID, tc.stratum)
			if got != tc.want {
				t.Errorf("ClassifySource(%q, %d) = %s, want %s",
					tc.refID, tc.stratum, got, tc.want)
			}
		})
	}
}

func itoa(u uint16) string {
	const digits = "0123456789"
	if u == 0 {
		return "0"
	}
	var b [5]byte
	i := len(b)
	for u > 0 {
		i--
		b[i] = digits[u%10]
		u /= 10
	}
	return string(b[i:])
}
