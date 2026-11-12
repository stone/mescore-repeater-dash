package web

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"meshtui/internal/repeater"
)

func good() repeater.Sample {
	return repeater.Sample{
		At:      time.Now(),
		Core:    repeater.Core{BatteryMV: 4331, UptimeSecs: 18637},
		Radio:   repeater.Radio{NoiseFloor: -111, LastRSSI: -106, LastSNR: 4.25, TxAirSecs: 225, RxAirSecs: 235},
		Packets: repeater.Packets{Recv: 350, Sent: 325, FloodRx: 298, RecvErrors: 14},
	}
}

func newTest(t *testing.T, cfg Config) (*Server, *httptest.Server) {
	t.Helper()
	s, err := New(cfg, repeater.NewDemo(1))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func get(t *testing.T, url, user, pass string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

func exec(t *testing.T, ts *httptest.Server, body string, header bool) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/exec", strings.NewReader(body))
	req.SetBasicAuth("admin", "s3cret")
	req.Header.Set("Content-Type", "application/json")
	if header {
		req.Header.Set("X-Requested-With", "meshtui")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestConfigValidate(t *testing.T) {
	for _, c := range []struct {
		cfg Config
		ok  bool
	}{
		{Config{Addr: ":8080"}, true},
		{Config{Addr: ":8080", User: "a", Pass: "b"}, true},
		{Config{Addr: ":8080", User: "a", Pass: "b", Console: true}, true},
		{Config{Addr: ":8080", User: "a"}, false},
		{Config{Addr: ":8080", Pass: "b"}, false},
		{Config{Addr: ":8080", Console: true}, false},
	} {
		if err := c.cfg.Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: err=%v want ok=%v", c.cfg, err, c.ok)
		}
	}
}

func TestBasicAuth(t *testing.T) {
	_, ts := newTest(t, Config{User: "admin", Pass: "s3cret"})
	for _, path := range []string{"/", "/app.js", "/api/state", "/api/events"} {
		if r, _ := get(t, ts.URL+path, "", ""); r.StatusCode != 401 || !strings.Contains(r.Header.Get("WWW-Authenticate"), "Basic") {
			t.Fatalf("%s without auth: %d", path, r.StatusCode)
		}
		if r, _ := get(t, ts.URL+path, "admin", "wrong"); r.StatusCode != 401 {
			t.Fatalf("%s wrong pass: %d", path, r.StatusCode)
		}
	}
	r, body := get(t, ts.URL+"/", "admin", "s3cret")
	if r.StatusCode != 200 || !strings.Contains(body, "<title>meshtui") {
		t.Fatalf("with auth: %d %.80q", r.StatusCode, body)
	}
}

func TestOpenWithoutAuthConfig(t *testing.T) {
	_, ts := newTest(t, Config{})
	if r, _ := get(t, ts.URL+"/", "", ""); r.StatusCode != 200 {
		t.Fatalf("got %d", r.StatusCode)
	}
}

func TestStaticEmbedded(t *testing.T) {
	_, ts := newTest(t, Config{})
	for path, ctype := range map[string]string{"/app.js": "javascript", "/style.css": "text/css"} {
		r, body := get(t, ts.URL+path, "", "")
		if r.StatusCode != 200 || !strings.Contains(r.Header.Get("Content-Type"), ctype) || len(body) < 100 {
			t.Fatalf("%s: %d %s len %d", path, r.StatusCode, r.Header.Get("Content-Type"), len(body))
		}
	}
}

func TestStateJSON(t *testing.T) {
	s, ts := newTest(t, Config{})
	s.SetInfo(repeater.Info{Name: "SE-KSD-SkareRep", Firmware: "v1.18.0", FreqMHz: 869.618, SF: 8})
	s.Observe(good())
	_, body := get(t, ts.URL+"/api/state", "", "")
	var st struct {
		Info    struct{ Name string }
		Up      bool
		Console bool
		Latest  struct {
			Core struct {
				BatteryMV int `json:"battery_mv"`
			}
		}
		History struct{ Noise []float64 }
		Help    []struct{ Key string }
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	if st.Info.Name != "SE-KSD-SkareRep" || !st.Up || st.Console || st.Latest.Core.BatteryMV != 4331 ||
		len(st.History.Noise) != 1 || st.History.Noise[0] != -111 || len(st.Help) == 0 {
		t.Fatalf("state %+v", st)
	}
}

func TestEventStream(t *testing.T) {
	s, ts := newTest(t, Config{})
	resp, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type %q", resp.Header.Get("Content-Type"))
	}
	rd := bufio.NewReader(resp.Body)
	next := func() (event, data string) {
		for {
			l, err := rd.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			l = strings.TrimRight(l, "\n")
			switch {
			case strings.HasPrefix(l, "event: "):
				event = l[7:]
			case strings.HasPrefix(l, "data: "):
				data = l[6:]
			case l == "" && event != "":
				return
			}
		}
	}
	if ev, _ := next(); ev != "state" {
		t.Fatalf("first event %q", ev)
	}
	go s.Observe(good())
	ev, data := next()
	if ev != "update" || !strings.Contains(data, `"battery_mv":4331`) || !strings.Contains(data, `"noise":-111`) {
		t.Fatalf("got %s %s", ev, data)
	}
}

func TestExecDisabledByDefault(t *testing.T) {
	_, ts := newTest(t, Config{User: "admin", Pass: "s3cret"})
	if code, _ := exec(t, ts, `{"cmd":"get tx"}`, true); code != 403 {
		t.Fatalf("got %d", code)
	}
}

func TestExecRequiresCSRFHeader(t *testing.T) {
	_, ts := newTest(t, Config{User: "admin", Pass: "s3cret", Console: true})
	if code, _ := exec(t, ts, `{"cmd":"get tx"}`, false); code != 403 {
		t.Fatalf("got %d", code)
	}
}

func TestExecReadOnlyRuns(t *testing.T) {
	s, ts := newTest(t, Config{User: "admin", Pass: "s3cret", Console: true})
	code, out := exec(t, ts, `{"cmd":"get tx"}`, true)
	if code != 200 || out["out"] != "> demo-tx" {
		t.Fatalf("got %d %v", code, out)
	}
	_, body := get(t, ts.URL+"/api/state", "admin", "s3cret")
	var st struct{ Events []struct{ Kind, Text string } }
	json.Unmarshal([]byte(body), &st)
	if n := len(st.Events); n == 0 || st.Events[n-1].Kind != "cmd" || st.Events[n-1].Text != "get tx → > demo-tx" {
		t.Fatalf("command not logged: %+v", st.Events)
	}
	_ = s
}

func TestExecRiskyNeedsConfirm(t *testing.T) {
	_, ts := newTest(t, Config{User: "admin", Pass: "s3cret", Console: true})
	code, out := exec(t, ts, `{"cmd":"reboot"}`, true)
	if code != 409 || out["needs_confirm"] != true {
		t.Fatalf("got %d %v", code, out)
	}
	code, out = exec(t, ts, `{"cmd":"reboot","confirm":true}`, true)
	if code != 200 || !strings.Contains(out["out"].(string), "demo") {
		t.Fatalf("got %d %v", code, out)
	}
}

func TestExecRejectsBadInput(t *testing.T) {
	_, ts := newTest(t, Config{User: "admin", Pass: "s3cret", Console: true})
	for _, b := range []string{`{"cmd":""}`, `not json`, `{"cmd":"ver\nreboot","confirm":true}`} {
		if code, _ := exec(t, ts, b, true); code != 400 {
			t.Fatalf("%s: got %d", b, code)
		}
	}
}

func TestSSEClientCap(t *testing.T) {
	s, ts := newTest(t, Config{})
	s.maxClients = 2
	var bodies []io.Closer
	defer func() {
		for _, b := range bodies {
			b.Close()
		}
	}()
	for i := 0; i < 2; i++ {
		resp, err := http.Get(ts.URL + "/api/events")
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("client %d: %v %v", i, err, resp.StatusCode)
		}
		bodies = append(bodies, resp.Body)
		bufio.NewReader(resp.Body).ReadString('\n') // wait until registered
	}
	resp, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("third client: %d, want 503", resp.StatusCode)
	}
}

func TestExecRejectsControlChars(t *testing.T) {
	_, ts := newTest(t, Config{User: "admin", Pass: "s3cret", Console: true})
	for _, b := range []string{`{"cmd":"get x\u0000reboot"}`, `{"cmd":"` + strings.Repeat("a", 200) + `"}`} {
		if code, _ := exec(t, ts, b, true); code != 400 {
			t.Fatalf("%.40s: got %d", b, code)
		}
	}
}

func TestPasswordRedactedInEvents(t *testing.T) {
	_, ts := newTest(t, Config{User: "admin", Pass: "s3cret", Console: true})
	exec(t, ts, `{"cmd":"password hunter2","confirm":true}`, true)
	_, body := get(t, ts.URL+"/api/state", "admin", "s3cret")
	if strings.Contains(body, "hunter2") {
		t.Fatalf("password leaked into events: %s", body)
	}
}

func TestResolvePassword(t *testing.T) {
	dir := t.TempDir()
	f := dir + "/pw"
	os.WriteFile(f, []byte("fromfile\n"), 0o600)
	env := func(v string) func(string) string { return func(string) string { return v } }
	for _, c := range []struct {
		flag, file, env, want string
		err                   bool
	}{
		{"flag", "", "", "flag", false},
		{"", f, "", "fromfile", false},
		{"", "", "fromenv", "fromenv", false},
		{"flag", f, "", "", true},
		{"", dir + "/missing", "", "", true},
		{"", "", "", "", false},
	} {
		got, err := ResolvePassword(c.flag, c.file, env(c.env))
		if (err != nil) != c.err || got != c.want {
			t.Errorf("%+v: got %q err %v", c, got, err)
		}
	}
}

func TestServeTLS(t *testing.T) {
	cert, key := selfSigned(t)
	cfg := Config{Addr: "127.0.0.1:0", CertFile: cert, KeyFile: key}
	if err := (Config{CertFile: cert}).Validate(); err == nil {
		t.Fatal("cert without key must fail")
	}
	s, err := New(cfg, repeater.NewDemo(1))
	if err != nil {
		t.Fatal(err)
	}
	addr, err := s.Serve()
	if err != nil {
		t.Fatal(err)
	}
	cl := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := cl.Get("https://" + addr.String() + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.TLS == nil {
		t.Fatalf("got %d tls=%v", resp.StatusCode, resp.TLS != nil)
	}
}

func selfSigned(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	dir := t.TempDir()
	certFile, keyFile = dir+"/c.pem", dir+"/k.pem"
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600)
	return
}

func TestAccessLog(t *testing.T) {
	var acc bytes.Buffer
	_, ts := newTest(t, Config{User: "u", Pass: "p", AccessLog: &acc})
	get(t, ts.URL+"/api/state", "u", "p")
	get(t, ts.URL+"/api/state", "u", "wrong")
	lines := strings.Split(strings.TrimSpace(acc.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 access lines, got %d:\n%s", len(lines), acc.String())
	}
	if !strings.Contains(lines[0], ` u [`) || !strings.Contains(lines[0], `"GET /api/state HTTP/1.1" 200 `) {
		t.Errorf("bad line for authorized request: %s", lines[0])
	}
	// a failed login must not log the attempted user name (it may be a mistyped password)
	if !strings.Contains(lines[1], ` - [`) || !strings.Contains(lines[1], `" 401 `) {
		t.Errorf("bad line for unauthorized request: %s", lines[1])
	}
}

func TestAccessLogKeepsSSEStreaming(t *testing.T) {
	var acc bytes.Buffer
	_, ts := newTest(t, Config{AccessLog: &syncWriter{w: &acc}})
	resp, err := http.Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "retry:") {
		t.Fatalf("SSE not streamed through access logger: %q %v", line, err)
	}
}

func TestExecAuditLog(t *testing.T) {
	var errl bytes.Buffer
	_, ts := newTest(t, Config{User: "admin", Pass: "s3cret", Console: true, ErrorLog: log.New(&errl, "", 0)})
	exec(t, ts, `{"cmd":"password hunter2","confirm":true}`, true)
	got := errl.String()
	if !strings.Contains(got, "user=admin") || !strings.Contains(got, "password") {
		t.Errorf("exec not audited: %q", got)
	}
	if strings.Contains(got, "hunter2") {
		t.Errorf("secret leaked into log: %q", got)
	}
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
