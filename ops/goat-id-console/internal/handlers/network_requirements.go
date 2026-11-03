package handlers

import (
	"log/slog"
	"net/http"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/mailer"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/welcome"
)

// networkRequirements is what a device on this goatnet needs outbound, from
// the same config facts the guide's firewall topic uses. Names and ports only.
func (s *Server) networkRequirements() (welcome.NetworkRequirements, bool) {
	return welcome.NetworkRequirementsFrom(s.Cfg.NotifyMgmtURL, s.Cfg.NotifyRelayAddresses, s.Cfg.NotifyStunHosts)
}

// handleNetworkRequirements serves the public "what your network must allow"
// page. Unauthenticated by design: it is meant to be read by a participant's
// network team BEFORE anyone has mesh access, and it carries only host names
// already in public DNS and port numbers — no resolved addresses.
func (s *Server) handleNetworkRequirements(w http.ResponseWriter, r *http.Request) {
	n, ok := s.networkRequirements()
	html, err := welcome.RenderNetworkRequirementsPage(s.Cfg.NotifyGoatnet, n, ok)
	if err != nil {
		slog.Error("network requirements render failed", "err", err)
		http.Error(w, "network requirements page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300")
	//nolint:errcheck // HTTP response delivery is best-effort after the handler has selected its result.
	_, _ = w.Write([]byte(html)) //nolint:gosec // rendered by html/template from config facts
}

// previewNetwork is the intake form's "Before you start: your network" data.
type previewNetwork struct {
	ControlHost  string   `json:"control_host"`
	ControlPorts []string `json:"control_ports"`
	DataHosts    []string `json:"data_hosts"`
	DataPorts    []string `json:"data_ports"`
	Test         string   `json:"test,omitempty"`
	Symptom      string   `json:"symptom"`
	PageURL      string   `json:"page_url,omitempty"`
}

func (s *Server) previewNetwork() *previewNetwork {
	n, ok := s.networkRequirements()
	if !ok {
		return nil
	}
	return &previewNetwork{
		ControlHost: n.ControlHost, ControlPorts: n.ControlPorts,
		DataHosts: n.DataHosts, DataPorts: n.DataPorts,
		Test: n.TestCommand(), Symptom: welcome.BlockedSymptom,
		// Relative: the form is served on the intake host, which proxies this path.
		PageURL: "/network-requirements",
	}
}

// newMailer builds the console's mailer with the invite's network block wired
// from this console's relay config, linking the public page on the same intake
// host the invite link points at.
func newMailer(cfg *config.Config) *mailer.Mailer {
	m := mailer.New(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFrom, cfg.SMTPReplyTo)
	n, ok := welcome.NetworkRequirementsFrom(cfg.NotifyMgmtURL, cfg.NotifyRelayAddresses, cfg.NotifyStunHosts)
	if ok {
		m.SetNetworkNote(func(goatnet, link string) []string {
			return n.EmailLines(goatnet, welcome.NetworkPageURL(link))
		})
	}
	return m
}
