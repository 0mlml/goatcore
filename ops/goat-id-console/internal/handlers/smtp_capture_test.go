package handlers

import (
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/mailer"
)

// captureMail uses the real SMTP client so a passing refusal test proves that
// no message was delivered, while the successful control exercises delivery.
func captureMail(t *testing.T, s *Server) <-chan string {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	messages := make(chan string, 20)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			func() {
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				c := textproto.NewConn(conn)
				if err := c.PrintfLine("220 localhost SMTP"); err != nil {
					t.Error(err)
					return
				}
				for {
					line, err := c.ReadLine()
					if err != nil {
						t.Error(err)
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						if err := c.PrintfLine("250-localhost\r\n250 AUTH PLAIN"); err != nil {
							t.Error(err)
							return
						}
						continue
					case strings.HasPrefix(line, "AUTH"):
						if err := c.PrintfLine("235 authenticated"); err != nil {
							t.Error(err)
							return
						}
						continue
					case strings.HasPrefix(line, "DATA"):
						if err := c.PrintfLine("354 send message"); err != nil {
							t.Error(err)
							return
						}
						msg, err := c.ReadDotBytes()
						if err != nil {
							t.Error(err)
							return
						}
						messages <- string(msg)
					case line == "QUIT":
						if err := c.PrintfLine("221 bye"); err != nil {
							t.Error(err)
						}
						return
					}
					if err := c.PrintfLine("250 OK"); err != nil {
						t.Error(err)
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		<-done
	})
	host, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	s.Mailer = mailer.New(host, port, "", "", "onboard@example.test", "admin@example.test")
	return messages
}
