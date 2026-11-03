package chrony

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	fbchrony "github.com/facebook/time/ntp/chrony"
)

const defaultSocketPath = "/run/chrony/chronyd.sock"

// chronyQueryTimeout bounds a single chronyd round-trip. chronyd's control
// socket is local and answers in well under a millisecond, so any wait beyond
// this means the reply is not coming (e.g. a perms/AppArmor misconfiguration on
// the reply path) — surface it as an error driving pollLoop's back-off rather
// than blocking the poll loop forever.
const chronyQueryTimeout = 5 * time.Second

// Re-export the types and constants from the upstream package that other
// internal packages reference, so callers don't need to import facebook/time.

type Tracking = fbchrony.Tracking
type SourceData = fbchrony.SourceData

const SourceStateSync = fbchrony.SourceStateSync
const SourceModeRef = fbchrony.SourceModeRef

var RefidToString = fbchrony.RefidToString

// Conn wraps a per-query unixgram connection to chronyd's control socket.
type Conn struct {
	socketPath string
}

// New returns a Conn pointing at the given chronyd socket (default if empty).
func New(socketPath string) *Conn {
	if socketPath == "" {
		socketPath = defaultSocketPath
	}
	return &Conn{socketPath: socketPath}
}

// GetTracking queries chronyd for its tracking data (current sync status).
func (c *Conn) GetTracking() (*Tracking, error) {
	client, cleanup, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	reply, err := client.Communicate(fbchrony.NewTrackingPacket())
	if err != nil {
		return nil, fmt.Errorf("tracking request: %w", err)
	}
	rt, ok := reply.(*fbchrony.ReplyTracking)
	if !ok {
		return nil, fmt.Errorf("unexpected reply type %T", reply)
	}
	return &rt.Tracking, nil
}

// GetSources queries chronyd for the list of time sources and their states.
func (c *Conn) GetSources() ([]*SourceData, error) {
	client, cleanup, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	reply, err := client.Communicate(fbchrony.NewSourcesPacket())
	if err != nil {
		return nil, fmt.Errorf("sources request: %w", err)
	}
	rs, ok := reply.(*fbchrony.ReplySources)
	if !ok {
		return nil, fmt.Errorf("unexpected reply type %T", reply)
	}
	n := rs.NSources
	sources := make([]*SourceData, 0, n)
	for i := range n {
		r2, err := client.Communicate(fbchrony.NewSourceDataPacket(int32(i)))
		if err != nil {
			return nil, fmt.Errorf("source-data[%d]: %w", i, err)
		}
		rsd, ok := r2.(*fbchrony.ReplySourceData)
		if !ok {
			return nil, fmt.Errorf("unexpected reply type %T for source-data[%d]", r2, i)
		}
		sd := rsd.SourceData
		sources = append(sources, &sd)
	}
	return sources, nil
}

// dial opens a per-query unixgram socket pair to chronyd.
// Returns a ready Client and a cleanup func.
//
// The client socket is bound in the SAME directory as chronyd's control socket
// (normally /run/chrony), NOT in /tmp. chronyd replies to a command by sending
// a datagram back to the client's bound address, and on stock Ubuntu / Ubuntu-
// for-Tegra chronyd runs under an AppArmor profile (enforce mode) that grants
// it `@{run}/chrony/{,*} rw` and nothing under /tmp. A client socket in /tmp is
// therefore unreachable to chronyd's reply path, the request fails with EACCES
// at connect time, and the agent silently degrades every verdict to
// ntp_class=unknown. Binding alongside the server socket mirrors what chronyc
// itself does and keeps the round-trip inside the profile-permitted directory.
func (c *Conn) dial() (*fbchrony.Client, func(), error) {
	localPath := filepath.Join(
		filepath.Dir(c.socketPath),
		fmt.Sprintf(".ntp-health-agent-%d.sock", os.Getpid()),
	)
	_ = os.Remove(localPath)

	laddr := &net.UnixAddr{Name: localPath, Net: "unixgram"}
	raddr := &net.UnixAddr{Name: c.socketPath, Net: "unixgram"}

	conn, err := net.DialUnix("unixgram", laddr, raddr)
	if err != nil {
		return nil, nil, fmt.Errorf("dial chronyd socket %s: %w", c.socketPath, err)
	}

	cleanup := func() {
		_ = conn.Close() //nolint:errcheck // cleanup after a failed setup preserves the setup error
		_ = os.Remove(localPath)
	}

	// chronyd drops privileges to the unprivileged _chrony user and replies to a
	// command by writing a datagram back to this client socket, which we created
	// root-owned. Make it group/other-writable so chronyd can deliver the reply —
	// otherwise the request is sent, no reply ever arrives, and the query hangs.
	// This mirrors chronyc, which chmods its own client socket. The socket lives
	// inside the 0700 /run/chrony directory, so 0666 here is not a wider exposure.
	if err := os.Chmod(localPath, 0o666); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("chmod client socket %s: %w", localPath, err)
	}

	// Bound the round-trip: a reply that never comes (e.g. a future reply-path
	// regression) becomes a query error driving back-off, not a silent hang.
	if err := conn.SetDeadline(time.Now().Add(chronyQueryTimeout)); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("set chrony client deadline: %w", err)
	}

	return &fbchrony.Client{Connection: conn}, cleanup, nil
}
