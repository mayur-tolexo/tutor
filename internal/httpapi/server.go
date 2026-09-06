// Package httpapi exposes the tutor over HTTP: content, attempts, hints,
// progress, identity, and the embedded PWA.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/mayur-tolexo/tutor/internal/auth"
	"github.com/mayur-tolexo/tutor/internal/content"
	"github.com/mayur-tolexo/tutor/internal/runner"
	"github.com/mayur-tolexo/tutor/internal/store"
	"github.com/mayur-tolexo/tutor/internal/tutor"
)

// Limits are the abuse controls applied per device, per IP, and per payload.
type Limits struct {
	RunsPerMinute      int
	RunsPerDayAnon     int
	RunsPerDaySignedIn int
	HintsPerDayAnon    int
	HintsPerDaySigned  int
	IPRunsPerMinute    int
	MaxCodeBytes       int
	MaxStdinBytes      int
}

// DefaultLimits are the values the design settled on for launch.
var DefaultLimits = Limits{
	RunsPerMinute: 30, RunsPerDayAnon: 500, RunsPerDaySignedIn: 2000,
	HintsPerDayAnon: 60, HintsPerDaySigned: 200, IPRunsPerMinute: 200,
	MaxCodeBytes: 32 << 10, MaxStdinBytes: 8 << 10,
}

// Server wires the domain packages to HTTP. All fields except Google* are
// required.
type Server struct {
	Library        *content.Library
	Runner         runner.Runner
	Store          store.Store
	Tutor          *tutor.Service
	Sessions       *auth.Sessions
	Google         auth.GoogleVerifier // nil disables sign-in
	GoogleClientID string
	Limits         Limits
	Static         fs.FS // built PWA; nil serves API only
	Log            *slog.Logger
	Now            func() time.Time

	minute  *bucketLimiter
	metrics *metrics
}

// Handler builds the routed http.Handler. It must be called once.
func (s *Server) Handler() http.Handler {
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Log == nil {
		s.Log = slog.Default()
	}
	if s.Limits == (Limits{}) {
		s.Limits = DefaultLimits
	}
	s.minute = newBucketLimiter(time.Minute)
	s.metrics = newMetrics()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.Handle("GET /metrics", s.metrics.handler())
	mux.HandleFunc("GET /v1/config", s.handleConfig)
	mux.HandleFunc("GET /v1/tracks", s.handleTracks)
	mux.HandleFunc("GET /v1/exercises/{id...}", s.handleExercise)
	mux.Handle("POST /v1/attempts", s.withIdentity(s.handleAttempt))
	mux.Handle("POST /v1/hints", s.withIdentity(s.handleHint))
	mux.Handle("GET /v1/progress", s.withIdentity(s.handleProgress))
	mux.Handle("GET /v1/me", s.withIdentity(s.handleMe))
	mux.Handle("PUT /v1/me", s.withIdentity(s.handleUpdateMe))
	mux.Handle("POST /v1/auth/google", s.withIdentity(s.handleGoogle))
	mux.Handle("POST /v1/auth/logout", s.withIdentity(s.handleLogout))
	if s.Static != nil {
		mux.Handle("/", spaHandler(s.Static))
	}
	return s.logRequests(mux)
}

// apiError is the wire shape of every failure.
type apiError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retry_after_seconds,omitempty"`
}

// httpError pairs a status with the body; handlers return it and the helpers
// serialise it.
type httpError struct {
	status int
	apiError
}

func (e *httpError) Error() string { return e.Code + ": " + e.Message }

func errInvalid(msg string) *httpError {
	return &httpError{http.StatusBadRequest, apiError{Code: "invalid", Message: msg}}
}
func errNotFound(msg string) *httpError {
	return &httpError{http.StatusNotFound, apiError{Code: "not_found", Message: msg}}
}
func errUnauthorized(msg string) *httpError {
	return &httpError{http.StatusUnauthorized, apiError{Code: "unauthorized", Message: msg}}
}
func errRateLimited(retry int) *httpError {
	return &httpError{http.StatusTooManyRequests, apiError{Code: "rate_limited", Message: "too many requests", RetryAfter: retry}}
}
func errPoolBusy() *httpError {
	return &httpError{http.StatusServiceUnavailable, apiError{Code: "pool_busy", Message: "servers are busy, please retry", RetryAfter: 2}}
}
func errInfra(msg string) *httpError {
	return &httpError{http.StatusBadGateway, apiError{Code: "infra_error", Message: msg}}
}

// writeJSON serialises v with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError maps any error to the wire shape; unknown errors become a 500
// with a generic message so internals never leak.
func (s *Server) writeError(w http.ResponseWriter, err error) {
	var he *httpError
	if !errors.As(err, &he) {
		s.Log.Error("internal error", "err", err)
		he = &httpError{http.StatusInternalServerError, apiError{Code: "internal", Message: "something went wrong"}}
	}
	if he.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(he.RetryAfter))
	}
	writeJSON(w, he.status, map[string]apiError{"error": he.apiError})
}

// decodeJSON reads a bounded JSON body into v.
func decodeJSON(r *http.Request, v any, limit int64) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return errInvalid("could not read body")
	}
	if int64(len(body)) > limit {
		return errInvalid("body too large")
	}
	if err := json.Unmarshal(body, v); err != nil {
		return errInvalid("malformed JSON")
	}
	return nil
}

// identity is who the request is from, resolved once per request.
type identity struct {
	Device  store.Device
	Student *store.Student
}

// Owner is the progress/hint owner: the student when linked, else the device.
func (id identity) Owner() store.Owner {
	if id.Student != nil {
		return store.Owner{Kind: store.OwnerStudent, ID: id.Student.ID}
	}
	return store.Owner{Kind: store.OwnerDevice, ID: id.Device.ID}
}

// Lang is the effective tutor language preference.
func (id identity) Lang() string {
	if id.Student != nil && id.Student.LangPref != "" {
		return id.Student.LangPref
	}
	if id.Device.LangPref != "" {
		return id.Device.LangPref
	}
	return "hinglish"
}

// withIdentity requires a valid X-Device-Id, touches the device, and attaches
// the signed-in student when a valid session cookie is present. A session
// naming a student that no longer exists is treated as signed out.
func (s *Server) withIdentity(next func(http.ResponseWriter, *http.Request, identity) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		devID := r.Header.Get("X-Device-Id")
		if !auth.ValidDeviceID(devID) {
			s.writeError(w, errInvalid("missing or malformed X-Device-Id"))
			return
		}
		dev, err := s.Store.TouchDevice(r.Context(), devID)
		if err != nil {
			s.writeError(w, err)
			return
		}
		id := identity{Device: dev}
		if sid := s.Sessions.StudentID(r); sid != "" {
			st, err := s.Store.GetStudent(r.Context(), sid)
			switch {
			case err == nil:
				id.Student = &st
				// A device that signs in on a new browser gets linked on first use.
				if dev.StudentID != st.ID {
					if err := s.Store.LinkDevice(r.Context(), dev.ID, st.ID); err != nil {
						s.writeError(w, err)
						return
					}
				}
			case errors.Is(err, store.ErrNotFound):
				s.Sessions.Clear(w)
			default:
				s.writeError(w, err)
				return
			}
		}
		if err := next(w, r, id); err != nil {
			s.writeError(w, err)
		}
	})
}

// clientIP prefers the first X-Forwarded-For hop (set by our own proxy) and
// falls back to the peer address.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// day formats the usage-counter day key.
func (s *Server) day() string {
	return s.Now().UTC().Format("2006-01-02")
}

// logRequests emits one structured line per request.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			s.Log.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "ms", time.Since(start).Milliseconds())
		}
	})
}

// statusWriter captures the response status for logging.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// spaHandler serves the built PWA, falling back to index.html for client-side
// routes. Hashed assets get long cache lifetimes; the shell does not.
func spaHandler(static fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		f, err := static.Open(p)
		if err != nil {
			// Unknown path: the client router owns it.
			p = "index.html"
			if f, err = static.Open(p); err != nil {
				http.NotFound(w, r)
				return
			}
		}
		defer f.Close()
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if ct := mime.TypeByExtension(path.Ext(p)); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		io.Copy(w, f)
	})
}
