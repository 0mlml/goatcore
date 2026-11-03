package api

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// staticAsset is one embedded frontend file, prepared once when the server is
// constructed: the exact bytes served (display-lexicon already substituted, for
// HTML), a strong ETag over those bytes, and — where it pays — a gzip encoding
// of the same bytes.
//
// Precomputing is safe because the embed FS is immutable for the life of the
// process: the asset set cannot change without a new image, so there is nothing
// to invalidate. The lexicon is resolved from this process's PORTAL_LEX_* env at
// construction, which keeps the property the serve-time substitution was there
// for — one image serves every goatnet, tokens resolved per-deployment, never
// baked at build time. See docs/design/portal-lexicon.md.
type staticAsset struct {
	body        []byte
	gzipBody    []byte // nil when the type is not compressible or gzip did not pay
	etag        string // strong ETag over body, quoted
	gzipETag    string // distinct ETag for the gzip representation (RFC 9110 §8.8.1)
	contentType string
}

// compressibleTypes are the media types worth gzipping. Everything else
// (images, fonts, archives) is already compressed; running deflate over it
// costs bytes rather than saving them.
func compressible(contentType string) bool {
	base := contentType
	if i := strings.Index(base, ";"); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}
	switch {
	case strings.HasPrefix(base, "text/"):
		return true
	case base == "application/json", base == "application/xml",
		base == "application/javascript", base == "application/wasm",
		base == "application/yaml", base == "image/svg+xml":
		return true
	default:
		return false
	}
}

// gzipMinSize is the floor below which compression is not worth a Vary +
// Content-Encoding round trip; a gzip frame has ~20 bytes of overhead and tiny
// payloads routinely come out bigger.
const gzipMinSize = 512

func etagFor(b []byte, suffix string) string {
	sum := sha256.Sum256(b)
	return `"` + base64.RawURLEncoding.EncodeToString(sum[:16]) + suffix + `"`
}

func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil
	}
	if _, err := zw.Write(b); err != nil {
		return nil
	}
	if err := zw.Close(); err != nil {
		return nil
	}
	// Only keep it if it actually saves transfer.
	if buf.Len() >= len(b) {
		return nil
	}
	return buf.Bytes()
}

// buildStaticAssets walks the embedded frontend once and prepares every file
// for serving. HTML gets the display-lexicon substitution applied here rather
// than per-request.
func buildStaticAssets(fsys fs.FS, replacer *strings.Replacer) (map[string]*staticAsset, error) {
	assets := make(map[string]*staticAsset)
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		ct := mime.TypeByExtension(path.Ext(name))
		// Go's mime table has no YAML entry, so openapi.yaml went out as
		// octet-stream. RFC 9512 registered application/yaml.
		if ext := path.Ext(name); ext == ".yaml" || ext == ".yml" {
			ct = "application/yaml; charset=utf-8"
		}
		if strings.HasSuffix(name, ".html") {
			ct = "text/html; charset=utf-8"
			b = []byte(replacer.Replace(string(b)))
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		a := &staticAsset{body: b, etag: etagFor(b, ""), contentType: ct}
		if compressible(ct) && len(b) >= gzipMinSize {
			if gz := gzipBytes(b); gz != nil {
				a.gzipBody = gz
				a.gzipETag = etagFor(b, "-gz")
			}
		}
		assets[name] = a
		return nil
	})
	if err != nil {
		return nil, err
	}
	return assets, nil
}

// staticHandler serves the embedded frontend from the precomputed asset table.
//
// Three things it does that the stock http.FileServer did not, each of which
// was costing a full re-download of the whole page on every navigation:
//
//   - ETag + revalidation. embed.FS reports a zero modtime, so ServeContent
//     omitted Last-Modified and the stock handler set no ETag either — a client
//     had nothing to revalidate against and refetched all ~215 KB every time.
//   - gzip. The frontend is ~215 KB of HTML/CSS/JS uncompressed, ~73 KB gzipped.
//   - Cache-Control. Without it the caching behaviour was heuristic per browser.
//
// Cache-Control is `no-cache` (revalidate before use, not "do not store"): the
// assets are unversioned filenames — /app.js is /app.js across image bumps — so
// a max-age would serve stale JS after a deploy. Revalidation costs one
// conditional request, multiplexed with the others on the same HTTP/2
// connection, and a 304 carries no body.
func (s *Server) staticHandler() http.Handler {
	toks := s.cfg.Lexicon.Tokens()
	pairs := make([]string, 0, len(toks)*2)
	for k, v := range toks {
		pairs = append(pairs, k, v)
	}
	replacer := strings.NewReplacer(pairs...)

	fileSrv := http.FileServer(http.FS(s.staticFS))
	// A Server can be built without a frontend (every API-level test does), in
	// which case there is nothing to precompute and every request falls through.
	var assets map[string]*staticAsset
	if s.staticFS != nil {
		var err error
		if assets, err = buildStaticAssets(s.staticFS, replacer); err != nil {
			// A broken embed is a build defect, not a runtime condition. Fall
			// back to the stock file server rather than refusing to serve.
			assets = nil
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := assets[staticAssetName(r.URL.Path)]
		if a == nil {
			fileSrv.ServeHTTP(w, r)
			return
		}

		h := w.Header()
		h.Set("Content-Type", a.contentType)
		h.Set("Cache-Control", "no-cache")
		h.Set("Vary", "Accept-Encoding")

		if a.gzipBody != nil && acceptsGzip(r) {
			h.Set("Content-Encoding", "gzip")
			h.Set("ETag", a.gzipETag)
			if etagMatches(r.Header.Get("If-None-Match"), a.gzipETag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			h.Set("Content-Length", strconv.Itoa(len(a.gzipBody)))
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, _ = w.Write(a.gzipBody)
			}
			return
		}

		// Identity encoding: hand off to ServeContent, which handles
		// If-None-Match against the ETag set above plus Range and HEAD.
		h.Set("ETag", a.etag)
		http.ServeContent(w, r, path.Base(r.URL.Path), time.Time{}, bytes.NewReader(a.body))
	})
}

// staticAssetName maps a request path to the embedded file it should serve, or
// "" when there is no such asset (the caller then delegates to the stock file
// server for correct 404 / directory behavior). "/" → index.html, "/dir/" →
// dir/index.html, everything else → the cleaned path. path.Clean collapses any
// ".." so the lookup cannot escape the embed root.
func staticAssetName(p string) string {
	clean := strings.TrimPrefix(path.Clean("/"+p), "/")
	switch {
	case clean == "" || clean == ".":
		return "index.html"
	case strings.HasSuffix(p, "/"):
		return clean + "/index.html"
	default:
		return clean
	}
}

// acceptsGzip reports whether the client advertised gzip. A bare substring test
// is not enough: "gzip;q=0" is an explicit refusal.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), "gzip") {
			continue
		}
		for _, param := range fields[1:] {
			param = strings.TrimSpace(param)
			if strings.HasPrefix(param, "q=") && isZeroQuality(param[2:]) {
				return false
			}
		}
		return true
	}
	return false
}

func isZeroQuality(v string) bool {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return err == nil && f == 0
}

// etagMatches implements the If-None-Match comparison for our own strong tags:
// a "*" matches anything, otherwise any comma-separated candidate equal to the
// tag (ignoring a W/ weak prefix) is a hit.
func etagMatches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	if strings.TrimSpace(ifNoneMatch) == "*" {
		return true
	}
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}
