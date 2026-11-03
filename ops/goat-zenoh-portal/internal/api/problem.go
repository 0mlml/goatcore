package api

// Every error the portal writes is an RFC 9457 problem details body. The
// change is additive: the nested `error` object the first clients parse is
// still there, so nothing that reads `error.code` or `error.message` moves.
// See docs/adr/<n>-portal-rest-problem-details-and-api-version.md.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"unicode/utf8"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

const (
	problemContentType = "application/problem+json; charset=utf-8"
	problemTypePrefix  = "urn:goat:portal:problem:"
)

// Problem is the wire shape of every 4xx and 5xx the portal produces. Type,
// Title, Status, Detail and Instance are the RFC 9457 members; Code is the
// portal's short machine code, and Error is the legacy envelope.
type Problem struct {
	Type     string   `json:"type"`
	Title    string   `json:"title"`
	Status   int      `json:"status"`
	Detail   string   `json:"detail,omitempty"`
	Instance string   `json:"instance,omitempty"`
	Code     string   `json:"code"`
	Error    apiError `json:"error"`
}

// serverFaultDetail is what a 5xx says on the wire, by code. The message the
// handler passed is logged against the request id and never sent: those
// messages carry file paths, upstream addresses and database text.
var serverFaultDetail = map[string]string{
	"affiliation_repair_failed": "affiliation repair failed; inspect the audit record and preview again before retrying",
	"db":                        "the registry could not complete the request",
	"store":                     "the schema store could not complete the request",
	"zenoh":                     "the router could not complete the request",
	"panic":                     "internal server error",
	"config":                    "this portal is not configured for that operation",
	"registry_unavailable":      "registry probe failed",
}

// maxDetail bounds the detail member. A validation message quotes the input
// it refused, which is otherwise as long as the caller made it.
const maxDetail = 1024

// newProblem builds the body for a status and code. The request id set by
// middleware() is the instance, so a log line and a client report meet.
func newProblem(w http.ResponseWriter, status int, code, msg string) Problem {
	detail := clipDetail(msg, maxDetail)
	if status >= 500 {
		detail = serverFaultDetail[code]
		if detail == "" {
			detail = http.StatusText(status)
		}
		slog.Error("request failed",
			"request_id", w.Header().Get("X-Request-Id"),
			"status", status,
			"code", code,
			"err", msg,
		)
	}
	return Problem{
		Type:     problemTypePrefix + code,
		Title:    http.StatusText(status),
		Status:   status,
		Detail:   detail,
		Instance: w.Header().Get("X-Request-Id"),
		Code:     code,
		Error:    apiError{Code: code, Message: detail},
	}
}

// members is the problem as a map, for the one response that carries
// extension members beside the standard ones (the health 503).
func (p Problem) members() map[string]any {
	return map[string]any{
		"type":     p.Type,
		"title":    p.Title,
		"status":   p.Status,
		"detail":   p.Detail,
		"instance": p.Instance,
		"code":     p.Code,
		"error":    p.Error,
	}
}

func writeProblem(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError keeps the signature every handler already uses; the body is the
// problem shape from here on.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeProblem(w, status, newProblem(w, status, code, msg))
}

// writeDecodeError answers a request body readJSON refused: 413 when the body
// passed the size cap, 400 for anything else.
func writeDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, "decode", err.Error())
}

// writeRegistryError answers a registry write that failed: a validation
// failure (registry.ErrInvalid) is a 400 that quotes the message, anything
// else is a store fault and goes out as a 5xx whose text is the fixed detail
// for "db", with the driver's message in the log only.
func writeRegistryError(w http.ResponseWriter, err error) {
	if errors.Is(err, registry.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "validate", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "db", err.Error())
}

// writeBodyError is writeDecodeError for a handler that keeps its own 400
// code and message: 413 when the body passed the cap, the handler's own
// refusal otherwise.
func writeBodyError(w http.ResponseWriter, err error, code, msg string) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeDecodeError(w, err)
		return
	}
	writeError(w, http.StatusBadRequest, code, msg)
}

// stdlibProblem is the body written in place of the standard library's
// plain-text 404 and 405 under /api (statusWriter.WriteHeader).
func stdlibProblem(w http.ResponseWriter, status int) Problem {
	code := "not_found"
	msg := "no route for that path"
	if status == http.StatusMethodNotAllowed {
		code, msg = "method_not_allowed", "that method is not supported on this path"
	}
	return newProblem(w, status, code, msg)
}

// MarshalProblem is the wire form of a problem built outside a request, for
// the problem-pages command that serves constant bodies for the proxy.
func MarshalProblem(p Problem) []byte {
	b, _ := json.Marshal(p)
	return append(b, '\n')
}

// clipDetail cuts s to at most n runes, marking the cut.
func clipDetail(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "\u2026"
}
