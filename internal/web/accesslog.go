package web

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// statusWriter records the status and size of a response. It keeps
// http.Flusher and Unwrap so SSE and http.ResponseController still work.
type statusWriter struct {
	http.ResponseWriter
	status, size int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.size += n
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// accessLog writes one Combined Log Format line per request, plus the
// duration, when the request finishes (SSE streams log when they close).
func accessLog(out io.Writer, h http.Handler) http.Handler {
	var mu sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		h.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		user := "-"
		// Only log the user of an accepted login: a rejected one may be a mistyped password.
		if u, _, ok := r.BasicAuth(); ok && u != "" && sw.status != http.StatusUnauthorized {
			user = esc(u)
		}
		line := fmt.Sprintf("%s - %s [%s] \"%s %s %s\" %d %d \"%s\" \"%s\" %.3fs\n",
			host, user, start.Format("02/Jan/2006:15:04:05 -0700"),
			esc(r.Method), esc(r.RequestURI), esc(r.Proto), sw.status, sw.size,
			esc(r.Referer()), esc(r.UserAgent()), time.Since(start).Seconds())
		mu.Lock()
		io.WriteString(out, line)
		mu.Unlock()
	})
}

// esc quotes control characters, quotes and backslashes so one request is one log line.
func esc(s string) string {
	if s == "" {
		return "-"
	}
	q := strconv.Quote(s)
	return q[1 : len(q)-1]
}
