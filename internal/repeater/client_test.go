package repeater

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// scripted replays reply once any write happens; records writes.
type scripted struct {
	mu      sync.Mutex
	written bytes.Buffer
	reply   string
	ch      chan []byte
	eof     bool
}

func newScripted(reply string) *scripted { return &scripted{reply: reply, ch: make(chan []byte, 1)} }

func (s *scripted) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.written.Write(p)
	s.mu.Unlock()
	if s.reply != "" {
		s.ch <- []byte(s.reply)
	}
	return len(p), nil
}

func (s *scripted) Read(p []byte) (int, error) {
	if s.eof {
		return 0, io.EOF
	}
	b := <-s.ch
	return copy(p, b), nil
}

func (s *scripted) writes() string { s.mu.Lock(); defer s.mu.Unlock(); return s.written.String() }

func TestQueryStripsEchoAndNoise(t *testing.T) {
	s := newScripted("boot junk\r\nstats-core\r\n  -> {\"battery_mv\":4331,\"uptime_secs\":18637,\"errors\":0,\"queue_len\":0}\r\n")
	c := NewClient(s)
	got, err := c.Query("stats-core")
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"battery_mv":4331,"uptime_secs":18637,"errors":0,"queue_len":0}` {
		t.Fatalf("got %q", got)
	}
	if s.writes() != "stats-core\r" {
		t.Fatalf("wrote %q", s.writes())
	}
}

func TestQueryMultiline(t *testing.T) {
	c := NewClient(newScripted("neighbors\r\n  -> A1B2C3D4:120:20\r\nDEADBEEF:5:-8\r\n"))
	got, err := c.Query("neighbors")
	if err != nil {
		t.Fatal(err)
	}
	if got != "A1B2C3D4:120:20\nDEADBEEF:5:-8" {
		t.Fatalf("got %q", got)
	}
}

func TestQueryRejectsWrites(t *testing.T) {
	s := newScripted("x\r\n")
	c := NewClient(s)
	for _, cmd := range []string{"reboot", "set name x", "stats-core; reboot", "stats-core\rreboot"} {
		if _, err := c.Query(cmd); !errors.Is(err, ErrNotAllowed) {
			t.Fatalf("%q: want ErrNotAllowed, got %v", cmd, err)
		}
	}
	if s.writes() != "" {
		t.Fatalf("wrote %q", s.writes())
	}
}

func TestQueryTimeout(t *testing.T) {
	s := newScripted("") // never replies
	c := NewClient(s)
	start := time.Now()
	if _, err := c.Query("ver"); err == nil {
		t.Fatal("want error")
	}
	if d := time.Since(start); d > 2500*time.Millisecond {
		t.Fatalf("took %v", d)
	}
}

func TestQueryEOF(t *testing.T) {
	s := newScripted("")
	s.eof = true
	c := NewClient(s)
	if _, err := c.Query("ver"); err == nil {
		t.Fatal("want error")
	}
}

func TestQueryIgnoresStaleReplyBeforeEcho(t *testing.T) {
	c := NewClient(newScripted("  -> {\"noise_floor\":-111}\r\nstats-core\r\n  -> {\"battery_mv\":4331}\r\n"))
	got, err := c.Query("stats-core")
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"battery_mv":4331}` {
		t.Fatalf("got %q", got)
	}
}

func TestExecSendsAnyCommand(t *testing.T) {
	s := newScripted("set tx 22\r\n  -> OK\r\n")
	got, err := NewClient(s).Exec("set tx 22")
	if err != nil || got != "OK" || s.writes() != "set tx 22\r" {
		t.Fatalf("got %q err %v wrote %q", got, err, s.writes())
	}
}

func TestExecRejectsMultiline(t *testing.T) {
	s := newScripted("x\r\n")
	for _, cmd := range []string{"ver\rreboot", "ver\nreboot", "", "   "} {
		if _, err := NewClient(s).Exec(cmd); err == nil {
			t.Fatalf("%q: want error", cmd)
		}
	}
	if s.writes() != "" {
		t.Fatalf("wrote %q", s.writes())
	}
}

func TestIsReadOnly(t *testing.T) {
	for cmd, want := range map[string]bool{
		"ver": true, "clock": true, "neighbors": true, "log": true, "stats-core": true, "stats-radio": true,
		"get tx": true, "get radio": true, "  get name ": true,
		"set tx 22": false, "reboot": false, "advert": false, "erase": false, "clock sync": false,
		"password x": false, "log erase": false, "log start": false, "getx": false, "": false, "foo": false,
	} {
		if got := IsReadOnly(cmd); got != want {
			t.Errorf("IsReadOnly(%q)=%v want %v", cmd, got, want)
		}
	}
}

func TestDemoExec(t *testing.T) {
	out, err := NewDemo(1).Exec("get tx")
	if err != nil || out == "" {
		t.Fatalf("got %q %v", out, err)
	}
}

func TestExecRejectsControlCharsAndLongLines(t *testing.T) {
	s := newScripted("x\r\n")
	for _, cmd := range []string{"get x\x00reboot", "get x\x1b[2J", "ver\x7f", strings.Repeat("a", 161)} {
		if _, err := NewClient(s).Exec(cmd); err == nil {
			t.Fatalf("%q: want error", cmd)
		}
	}
	if s.writes() != "" {
		t.Fatalf("wrote %q", s.writes())
	}
	if err := ValidateCommand(strings.Repeat("a", 160)); err != nil {
		t.Fatalf("160 chars should be fine: %v", err)
	}
}

func TestRedact(t *testing.T) {
	for _, c := range []struct{ cmd, out, wantCmd, wantOut string }{
		{"password hunter2", "OK", "password ***", "OK"},
		{"set guest.password abc", "OK", "set guest.password ***", "OK"},
		{"get guest.password", "> abc", "get guest.password", "> ***"},
		{"get tx", "> 22", "get tx", "> 22"},
	} {
		if gc, gotOut := Redact(c.cmd, c.out); gc != c.wantCmd || gotOut != c.wantOut {
			t.Errorf("Redact(%q,%q) = %q,%q", c.cmd, c.out, gc, gotOut)
		}
	}
}
