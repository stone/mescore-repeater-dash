package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"meshtui/internal/repeater"
)

type deadSource struct{}

func (deadSource) Info() (repeater.Info, error) { return repeater.Info{Name: "?"}, nil }
func (deadSource) Poll(bool) repeater.Sample {
	return repeater.Sample{At: time.Now(), Err: errors.New("timeout")}
}
func (deadSource) Close() error                { return nil }
func (deadSource) Exec(string) (string, error) { return "", errors.New("timeout") }

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(msg)
	mm, ok := nm.(Model)
	if !ok {
		t.Fatalf("Update returned %T", nm)
	}
	return mm, cmd
}

func demoModel(t *testing.T, w, h int) Model {
	src := repeater.NewDemo(1)
	info, _ := src.Info()
	m := New(src, info, 2*time.Second)
	m, _ = step(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	base := time.Now()
	for i := 0; i < 10; i++ {
		s := src.Poll(i == 0)
		s.At = base.Add(time.Duration(i) * 2 * time.Second)
		m, _ = step(t, m, pollMsg{s})
		m, _ = step(t, m, frameMsg(s.At))
	}
	return m
}

func TestRenderSizes(t *testing.T) {
	for _, sz := range [][2]int{{40, 12}, {80, 24}, {120, 40}, {200, 60}} {
		m := demoModel(t, sz[0], sz[1])
		out := ansi.Strip(m.View().Content)
		if sz[0] == 40 {
			if !strings.Contains(out, "too small") {
				t.Fatalf("40x12: want too small, got %q", out)
			}
			continue
		}
		lines := strings.Split(out, "\n")
		if len(lines) != sz[1] {
			t.Fatalf("%v: %d lines, want exactly %d", sz, len(lines), sz[1])
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != sz[0] {
				t.Fatalf("%v: line %d is %d wide, want %d: %q", sz, i, w, sz[0], l)
			}
		}
		if sz[0] == 120 {
			for _, want := range []string{"SE-DEMO-Repeater", "Noise", "Packets"} {
				if !strings.Contains(out, want) {
					t.Fatalf("120x40 missing %q", want)
				}
			}
		}
	}
}

func key(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }

func TestKeys(t *testing.T) {
	m := demoModel(t, 120, 40)
	m, _ = step(t, m, key("p"))
	if !m.paused {
		t.Fatal("p should pause")
	}
	m, _ = step(t, m, key("p"))
	if m.paused {
		t.Fatal("p should unpause")
	}
	th := m.theme
	m, _ = step(t, m, key("t"))
	if m.theme == th {
		t.Fatal("t should change theme")
	}
	_, cmd := step(t, m, key("q"))
	if cmd == nil {
		t.Fatal("q should return a cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q should quit")
	}
}

func TestNoSignalBanner(t *testing.T) {
	m := New(deadSource{}, repeater.Info{Name: "?"}, 2*time.Second)
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	for i := 0; i < 3; i++ {
		m, _ = step(t, m, pollMsg{deadSource{}.Poll(false)})
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "NO SIGNAL") {
		t.Fatal("want NO SIGNAL banner")
	}
}

type lateInfoSource struct{ repeater.Source }

func TestInfoRetriedAfterGoodPoll(t *testing.T) {
	src := repeater.NewDemo(1)
	m := New(src, repeater.Info{Name: "?"}, time.Millisecond)
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	_, cmd := step(t, m, pollMsg{src.Poll(false)})
	var found *repeater.Info
	var walk func(c tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, cc := range msg {
				walk(cc)
			}
		case infoMsg:
			found = &msg.info
		}
	}
	walk(cmd)
	if found == nil || found.Name != "SE-DEMO-Repeater" {
		t.Fatalf("info not refetched: %+v", found)
	}
	m, _ = step(t, m, infoMsg{*found})
	if !strings.Contains(ansi.Strip(m.View().Content), "SE-DEMO-Repeater") {
		t.Fatal("header should show refetched name")
	}
}

func typeText(t *testing.T, m Model, s string) Model {
	for _, r := range s {
		m, _ = step(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

var enter = tea.KeyPressMsg{Code: tea.KeyEnter}
var escape = tea.KeyPressMsg{Code: tea.KeyEscape}

func runsExec(cmd tea.Cmd) (execMsg, bool) {
	if cmd == nil {
		return execMsg{}, false
	}
	switch msg := cmd().(type) {
	case execMsg:
		return msg, true
	case tea.BatchMsg:
		for _, c := range msg {
			if e, ok := runsExec(c); ok {
				return e, true
			}
		}
	}
	return execMsg{}, false
}

func TestHelpToggle(t *testing.T) {
	m := demoModel(t, 120, 40)
	m, _ = step(t, m, key("h"))
	if !strings.Contains(ansi.Strip(m.View().Content), "noise floor") {
		t.Fatal("help should describe the noise floor chart")
	}
	m, _ = step(t, m, escape)
	if strings.Contains(ansi.Strip(m.View().Content), "noise floor") {
		t.Fatal("esc should close help")
	}
}

func TestPromptReadOnlyRunsDirectly(t *testing.T) {
	m := demoModel(t, 120, 40)
	m, _ = step(t, m, key(":"))
	m = typeText(t, m, "get tx")
	m, cmd := step(t, m, enter)
	e, ok := runsExec(cmd)
	if !ok || e.cmd != "get tx" {
		t.Fatalf("want exec of get tx, got %+v %v", e, ok)
	}
	m, _ = step(t, m, e)
	if !strings.Contains(ansi.Strip(m.View().Content), "get tx") {
		t.Fatal("console should show the command")
	}
}

func TestPromptRiskyNeedsConfirm(t *testing.T) {
	m := demoModel(t, 120, 40)
	m, _ = step(t, m, key(":"))
	m = typeText(t, m, "reboot")
	m, cmd := step(t, m, enter)
	if _, ok := runsExec(cmd); ok {
		t.Fatal("risky command must not run before confirmation")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Send it?") {
		t.Fatal("want confirmation dialog")
	}
	m, cmd = step(t, m, key("n"))
	if _, ok := runsExec(cmd); ok {
		t.Fatal("n must cancel")
	}
	m = typeText(t, m, "reboot")
	m, _ = step(t, m, enter)
	_, cmd = step(t, m, key("y"))
	if e, ok := runsExec(cmd); !ok || e.cmd != "reboot" {
		t.Fatal("y must send")
	}
}

func TestPromptSwallowsQuitKey(t *testing.T) {
	m := demoModel(t, 120, 40)
	m, _ = step(t, m, key(":"))
	m, cmd := step(t, m, key("q"))
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("q in prompt must not quit")
		}
	}
	m, _ = step(t, m, escape)
	if m.mode != modeNormal {
		t.Fatal("esc should close the prompt")
	}
}

func TestPromptShowsPlaceholder(t *testing.T) {
	m := demoModel(t, 120, 40)
	m, _ = step(t, m, key(":"))
	if !strings.Contains(ansi.Strip(m.View().Content), "tab completes") {
		t.Fatal("empty prompt should show the placeholder")
	}
}

func TestMonokaiTheme(t *testing.T) {
	m := demoModel(t, 120, 40)
	for i := 0; i < len(themes); i++ {
		if m.th().Name == "Monokai" {
			if !strings.Contains(ansi.Strip(m.View().Content), "Monokai") {
				t.Fatal("footer should name the Monokai theme")
			}
			return
		}
		m, _ = step(t, m, key("t"))
	}
	t.Fatal("no Monokai theme reachable with t")
}
