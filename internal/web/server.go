// Package web serves the browser dashboard: embedded static files, a JSON
// snapshot, a Server-Sent Events stream of updates and an optional console.
package web

import (
	"crypto/subtle"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"meshtui/internal/help"
	"meshtui/internal/metrics"
	"meshtui/internal/repeater"
)

//go:embed static
var staticFS embed.FS

// Config is set from the command line.
type Config struct {
	Addr       string
	User, Pass string
	Console    bool // allow running repeater commands from the browser
	// Optional TLS; both or neither.
	CertFile, KeyFile string
	// AccessLog gets one line per request (off when nil). ErrorLog gets
	// server errors and an audit line per console command (discarded when nil).
	AccessLog io.Writer
	ErrorLog  *log.Logger
}

// ResolvePassword picks the password from -web-pass, -web-pass-file or the
// MESHTUI_WEB_PASS environment variable (keeps it out of `ps`).
func ResolvePassword(flag, file string, getenv func(string) string) (string, error) {
	if flag != "" && file != "" {
		return "", errors.New("use -web-pass or -web-pass-file, not both")
	}
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	if flag != "" {
		return flag, nil
	}
	return getenv("MESHTUI_WEB_PASS"), nil
}

// Validate rejects half-configured auth and an unauthenticated console.
func (c Config) Validate() error {
	if (c.User == "") != (c.Pass == "") {
		return errors.New("set both -web-user and -web-pass, or neither")
	}
	if c.Console && c.User == "" {
		return errors.New("-web-console needs -web-user and -web-pass")
	}
	if (c.CertFile == "") != (c.KeyFile == "") {
		return errors.New("set both -web-tls-cert and -web-tls-key, or neither")
	}
	return nil
}

// Server holds dashboard state fed by repeater.Tee and serves it over HTTP.
type Server struct {
	cfg Config
	src repeater.Source // for console Exec only

	mu         sync.Mutex
	info       repeater.Info
	st         *metrics.State
	times      *metrics.Ring
	lastOK     time.Time
	clients    map[chan []byte]struct{}
	maxClients int
}

// New validates cfg and returns a Server. src is used only to run console commands.
func New(cfg Config, src repeater.Source) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.ErrorLog == nil {
		cfg.ErrorLog = log.New(io.Discard, "", 0)
	}
	return &Server{cfg: cfg, src: src, st: metrics.NewState(), times: metrics.NewRing(900),
		clients: map[chan []byte]struct{}{}, maxClients: 32}, nil
}

// ---- JSON shapes ----

type infoJSON struct {
	Name     string  `json:"name"`
	Firmware string  `json:"firmware"`
	FreqMHz  float64 `json:"freq_mhz"`
	BWkHz    float64 `json:"bw_khz"`
	SF       int     `json:"sf"`
	CR       int     `json:"cr"`
}

type latestJSON struct {
	Core    repeater.Core    `json:"core"`
	Radio   repeater.Radio   `json:"radio"`
	Packets repeater.Packets `json:"packets"`
}

type neighborJSON struct {
	ID       string  `json:"id"`
	HeardAgo float64 `json:"heard_ago_s"`
	SNR      float64 `json:"snr"`
}

type eventJSON struct {
	T    int64  `json:"t"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type historyJSON struct {
	T       []float64 `json:"t"`
	Noise   []float64 `json:"noise"`
	RSSI    []float64 `json:"rssi"`
	SNR     []float64 `json:"snr"`
	Battery []float64 `json:"battery"`
	Rx      []float64 `json:"rx_per_min"`
	Tx      []float64 `json:"tx_per_min"`
	Duty    []float64 `json:"duty"`
}

type stateJSON struct {
	Info      infoJSON       `json:"info"`
	Up        bool           `json:"up"`
	Failures  int            `json:"failures"`
	LastOK    int64          `json:"last_ok"`
	Console   bool           `json:"console"`
	Latest    latestJSON     `json:"latest"`
	History   historyJSON    `json:"history"`
	Neighbors []neighborJSON `json:"neighbors"`
	Events    []eventJSON    `json:"events"`
	Help      []help.Section `json:"help"`
	Commands  []string       `json:"commands"`
}

type pointJSON struct {
	T       float64  `json:"t"`
	Noise   float64  `json:"noise"`
	RSSI    float64  `json:"rssi"`
	SNR     float64  `json:"snr"`
	Battery float64  `json:"battery"`
	Rx      *float64 `json:"rx_per_min,omitempty"`
	Tx      *float64 `json:"tx_per_min,omitempty"`
	Duty    *float64 `json:"duty,omitempty"`
}

type updateJSON struct {
	Up        bool           `json:"up"`
	Failures  int            `json:"failures"`
	LastOK    int64          `json:"last_ok"`
	Info      *infoJSON      `json:"info,omitempty"`
	Latest    *latestJSON    `json:"latest,omitempty"`
	Point     *pointJSON     `json:"point,omitempty"`
	NewRx     int            `json:"new_rx"`
	NewTx     int            `json:"new_tx"`
	Neighbors []neighborJSON `json:"neighbors,omitempty"`
	Events    []eventJSON    `json:"events,omitempty"`
}

var kindNames = map[metrics.EventKind]string{
	metrics.EvRx: "rx", metrics.EvTx: "tx", metrics.EvError: "error", metrics.EvReboot: "reboot",
	metrics.EvNeighbor: "neighbor", metrics.EvSignal: "signal", metrics.EvCmd: "cmd",
}

func toInfo(in repeater.Info) infoJSON {
	return infoJSON{in.Name, in.Firmware, in.FreqMHz, in.BWkHz, in.SF, in.CR}
}

func toEvents(ev []metrics.Event) []eventJSON {
	out := make([]eventJSON, 0, len(ev))
	for _, e := range ev {
		out = append(out, eventJSON{e.At.UnixMilli(), kindNames[e.Kind], e.Text})
	}
	return out
}

func toNeighbors(nb []repeater.Neighbor) []neighborJSON {
	out := make([]neighborJSON, 0, len(nb))
	for _, n := range nb {
		out = append(out, neighborJSON{n.ID, n.HeardAgo.Seconds(), n.SNR})
	}
	return out
}

func last(r *metrics.Ring) *float64 {
	v, _ := r.Last()
	return &v
}

func (s *Server) up() bool { return !s.lastOK.IsZero() && s.st.Failures < metrics.NoSignalAfter }

func unixMs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// snapshot must be called with s.mu held.
func (s *Server) snapshot() stateJSON {
	ev := s.st.Events
	if len(ev) > 100 {
		ev = ev[len(ev)-100:]
	}
	var hs []help.Section
	for _, h := range help.Sections {
		if h.Key != "keys" {
			hs = append(hs, h)
		}
	}
	x := s.st.Latest
	return stateJSON{
		Info: toInfo(s.info), Up: s.up(), Failures: s.st.Failures, LastOK: unixMs(s.lastOK), Console: s.cfg.Console,
		Latest: latestJSON{x.Core, x.Radio, x.Packets},
		History: historyJSON{
			T: s.times.Values(), Noise: s.st.Noise.Values(), RSSI: s.st.RSSI.Values(), SNR: s.st.SNR.Values(),
			Battery: s.st.BatteryV.Values(), Rx: s.st.RxPerMin.Values(), Tx: s.st.TxPerMin.Values(), Duty: s.st.DutyPct.Values(),
		},
		Neighbors: toNeighbors(s.st.Neighbors), Events: toEvents(ev), Help: hs, Commands: repeater.CommonCommands,
	}
}

// ---- repeater.Observer ----

// SetInfo records the repeater identity and pushes it to browsers.
func (s *Server) SetInfo(in repeater.Info) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.info = in
	ij := toInfo(in)
	s.broadcast("update", updateJSON{Up: s.up(), Failures: s.st.Failures, LastOK: unixMs(s.lastOK), Info: &ij})
}

// Observe folds a poll result into the state and pushes the delta to browsers.
func (s *Server) Observe(x repeater.Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	evs := s.st.Apply(x)
	u := updateJSON{NewRx: s.st.NewRx, NewTx: s.st.NewTx, Events: toEvents(evs)}
	if x.Err == nil {
		s.lastOK = x.At
		s.times.Push(float64(x.At.UnixMilli()) / 1000)
		u.Latest = &latestJSON{x.Core, x.Radio, x.Packets}
		p := &pointJSON{T: float64(x.At.UnixMilli()) / 1000, Noise: float64(x.Radio.NoiseFloor),
			RSSI: float64(x.Radio.LastRSSI), SNR: x.Radio.LastSNR, Battery: float64(x.Core.BatteryMV) / 1000}
		if s.st.Pushed.Rates {
			p.Rx, p.Tx = last(s.st.RxPerMin), last(s.st.TxPerMin)
		}
		if s.st.Pushed.Duty {
			p.Duty = last(s.st.DutyPct)
		}
		u.Point = p
		if x.HasNeighbors {
			u.Neighbors = toNeighbors(x.Neighbors)
			if u.Neighbors == nil || len(u.Neighbors) == 0 {
				u.Neighbors = []neighborJSON{}
			}
		}
	}
	u.Up, u.Failures, u.LastOK = s.up(), s.st.Failures, unixMs(s.lastOK)
	s.broadcast("update", u)
}

// broadcast must be called with s.mu held. Slow clients drop messages.
func (s *Server) broadcast(event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, b))
	for c := range s.clients {
		select {
		case c <- msg:
		default:
		}
	}
}

// ---- HTTP ----

// Handler returns the full HTTP handler, wrapped in basic auth when configured.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /", http.FileServerFS(sub))
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("POST /api/exec", s.handleExec)
	var h http.Handler = mux
	if s.cfg.User != "" {
		h = s.basicAuth(h)
	}
	h = securityHeaders(h)
	if s.cfg.AccessLog != nil {
		h = accessLog(s.cfg.AccessLog, h)
	}
	return h
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) basicAuth(h http.Handler) http.Handler {
	user, pass := []byte(s.cfg.User), []byte(s.cfg.Pass)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		uOK := subtle.ConstantTimeCompare([]byte(u), user) == 1
		pOK := subtle.ConstantTimeCompare([]byte(p), pass) == 1
		if !ok || !uOK || !pOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="meshtui", charset="UTF-8"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	st := s.snapshot()
	s.mu.Unlock()
	writeJSON(w, 200, st)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	c := make(chan []byte, 32)
	s.mu.Lock()
	if len(s.clients) >= s.maxClients {
		s.mu.Unlock()
		http.Error(w, "too many dashboard connections", http.StatusServiceUnavailable)
		return
	}
	snap := s.snapshot() // ring Values are copies, safe to marshal unlocked
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
	}()
	b, _ := json.Marshal(snap)
	rc := http.NewResponseController(w)
	send := func(p []byte) bool {
		// A client that stops reading must not pin this goroutine forever.
		rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := w.Write(p); err != nil {
			return false
		}
		return fl != nil && rc.Flush() == nil
	}
	if !send([]byte(fmt.Sprintf("retry: 3000\nevent: state\ndata: %s\n\n", b))) {
		return
	}
	hb := time.NewTicker(15 * time.Second)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-c:
			if !send(msg) {
				return
			}
		case <-hb.C:
			if !send([]byte(": ping\n\n")) {
				return
			}
		}
	}
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Console {
		writeJSON(w, 403, map[string]any{"error": "console disabled (start with -web-console)"})
		return
	}
	// CSRF: browsers can't send this header cross-origin without a CORS preflight we never allow.
	if r.Header.Get("X-Requested-With") != "meshtui" || !sameOrigin(r) {
		writeJSON(w, 403, map[string]any{"error": "cross-site request refused"})
		return
	}
	var req struct {
		Cmd     string `json:"cmd"`
		Confirm bool   `json:"confirm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"error": "bad request"})
		return
	}
	cmd := strings.TrimSpace(req.Cmd)
	if err := repeater.ValidateCommand(cmd); err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error()})
		return
	}
	if !repeater.IsReadOnly(cmd) && !req.Confirm {
		writeJSON(w, 409, map[string]any{"needs_confirm": true, "cmd": cmd})
		return
	}
	out, err := s.src.Exec(cmd)
	logCmd, logOut := repeater.Redact(cmd, out) // the event log is shown to every viewer
	res := strings.SplitN(logOut, "\n", 2)[0]
	if err != nil {
		res = "error: " + err.Error()
	}
	user, _, _ := r.BasicAuth()
	s.cfg.ErrorLog.Printf("exec user=%s from %s: %q → %q", user, r.RemoteAddr, logCmd, res)
	s.mu.Lock()
	ev := metrics.Event{At: time.Now(), Kind: metrics.EvCmd, Text: fmt.Sprintf("%s → %s", logCmd, res)}
	s.st.Log(ev.At, ev.Kind, ev.Text)
	s.broadcast("update", updateJSON{Up: s.up(), Failures: s.st.Failures, LastOK: unixMs(s.lastOK), Events: toEvents([]metrics.Event{ev})})
	s.mu.Unlock()
	if err != nil {
		writeJSON(w, 502, map[string]any{"cmd": cmd, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"cmd": cmd, "out": out})
}

func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && u.Host == r.Host
}

// Serve listens on cfg.Addr and serves in the background.
func (s *Server) Serve() (net.Addr, error) {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ErrorLog: s.cfg.ErrorLog}
	if s.cfg.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(s.cfg.CertFile, s.cfg.KeyFile)
		if err != nil {
			ln.Close()
			return nil, err
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		go srv.ServeTLS(ln, "", "")
		return ln.Addr(), nil
	}
	go srv.Serve(ln)
	return ln.Addr(), nil
}
