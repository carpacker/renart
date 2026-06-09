package httpapi

import (
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"go.uber.org/zap"
	webapi "renart/internal/web/api"
)

func isLoopbackHost(hostname string) bool {
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

// statusWriter records the response status code while preserving the
// http.Flusher behavior streaming handlers (SSE) depend on.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// RequestLogger logs API requests after completion. Static asset requests
// are skipped to keep the output focused on API traffic.
func RequestLogger(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			sw := &statusWriter{ResponseWriter: w}
			start := time.Now()
			next.ServeHTTP(sw, r)

			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			logger.Info("http",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", status),
				zap.Duration("duration", time.Since(start)),
			)
		})
	}
}

// Recoverer converts handler panics into logged 500 responses instead of
// killing the connection silently.
func Recoverer(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					logger.Error("panic in handler",
						zap.Any("panic", rec),
						zap.String("method", r.Method),
						zap.String("path", r.URL.Path),
						zap.ByteString("stack", debug.Stack()),
					)
					webapi.WriteInternalError(w, "internal_error", "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// SameOriginGuard rejects state-changing cross-origin browser requests.
// Renart executes SQL/Python and writes workspace files, so a malicious web
// page must not be able to fire no-preflight POSTs at the local server.
// Browsers attach an Origin header to cross-origin requests; non-browser
// clients (curl, CLI integrations) send none and remain unaffected.
func SameOriginGuard() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			parsed, err := url.Parse(origin)
			if err != nil || parsed.Host == "" {
				webapi.WriteError(w, http.StatusForbidden, "cross_origin_rejected", "cross-origin request rejected")
				return
			}

			// Loopback origins on any port are trusted so the Vite dev
			// server (which proxies /api with a rewritten Host header)
			// keeps working; web pages can never run on a loopback origin
			// unless something local already serves them.
			if strings.EqualFold(parsed.Host, r.Host) || isLoopbackHost(parsed.Hostname()) {
				next.ServeHTTP(w, r)
				return
			}

			webapi.WriteError(w, http.StatusForbidden, "cross_origin_rejected", "cross-origin request rejected")
		})
	}
}
