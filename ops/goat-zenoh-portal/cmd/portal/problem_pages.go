package main

// problem-pages serves the constant RFC 9457 bodies the reverse proxy
// substitutes for its own plain-text errors, the rate limiter's 429 first of
// all. It runs as its own container beside the portal, never as a route on
// the portal: a Traefik errors middleware fetches its page from the service
// directly, outside the router's limiters, so a page served by the portal
// would hand every shed request straight back to the backend.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/api"
)

// problemPages is the allowlist of statuses served. Anything else is a
// constant 404 page; nothing from the request is reflected.
var problemPages = map[int]api.Problem{
	http.StatusTooManyRequests: {
		Code:   "rate_limited",
		Detail: "too many requests from this client address; wait one second and retry",
	},
	http.StatusBadGateway: {
		Code:   "upstream_unavailable",
		Detail: "the portal did not answer",
	},
	http.StatusServiceUnavailable: {
		Code:   "unavailable",
		Detail: "the portal is not accepting requests",
	},
	http.StatusGatewayTimeout: {
		Code:   "upstream_timeout",
		Detail: "the portal did not answer in time",
	},
	http.StatusNotFound: {
		Code:   "not_found",
		Detail: "no problem page for that status",
	},
}

// problemPageHandler answers GET /<status>.json from the allowlist.
func problemPageHandler() http.Handler {
	pages := make(map[string][]byte, len(problemPages))
	for status, p := range problemPages {
		p.Type = "urn:goat:portal:problem:" + p.Code
		p.Title = http.StatusText(status)
		p.Status = status
		p.Error.Code, p.Error.Message = p.Code, p.Detail
		pages[strconv.Itoa(status)] = api.MarshalProblem(p)
	}
	notFound := pages["404"]
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "application/problem+json; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		status := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".json")
		body, ok := pages[status]
		if !ok || status == "404" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(notFound)
			return
		}
		code, _ := strconv.Atoi(status)
		if code == http.StatusTooManyRequests {
			// The browsing limiter's period.
			h.Set("Retry-After", "1")
		}
		w.WriteHeader(code)
		_, _ = w.Write(body)
	})
}

func cmdProblemPages(args []string) error {
	fs := flag.NewFlagSet("problem-pages", flag.ContinueOnError)
	listen := fs.String("listen", "0.0.0.0:8091", "address to serve the problem pages on")
	if err := fs.Parse(args); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("GET /", problemPageHandler())
	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	slog.Info("problem-pages listening", "addr", *listen)
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("problem-pages: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
