package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"meshtui/internal/help"
	"meshtui/internal/metrics"
	"meshtui/internal/repeater"
)

type mode int

const (
	modeNormal mode = iota
	modeHelp
	modePrompt
	modeConfirm
)

type execMsg struct {
	cmd, out string
	err      error
}

type consoleLine struct {
	at       time.Time
	cmd, out string
	err      bool
	waiting  bool
}

const maxConsole = 50

func newInput(th Theme) textinput.Model {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "repeater command — tab completes, e.g. get tx"
	ti.CharLimit = 160
	ti.ShowSuggestions = true
	ti.SetSuggestions(repeater.CommonCommands)
	ti.SetVirtualCursor(true)
	st := textinput.DefaultDarkStyles()
	st.Focused.Prompt = fg(th.Title1).Bold(true)
	st.Focused.Text = fg(th.Fg)
	st.Focused.Placeholder = fg(th.Dim).Italic(true)
	st.Focused.Suggestion = fg(th.Dim)
	ti.SetStyles(st)
	return ti
}

func (m Model) execCmd(cmd string) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		out, err := src.Exec(cmd)
		return execMsg{cmd: cmd, out: out, err: err}
	}
}

func (m *Model) send(cmd string) tea.Cmd {
	m.console = append(m.console, consoleLine{at: m.now, cmd: cmd, waiting: true})
	if len(m.console) > maxConsole {
		m.console = m.console[len(m.console)-maxConsole:]
	}
	return m.execCmd(cmd)
}

func (m *Model) gotExec(e execMsg) {
	for i := len(m.console) - 1; i >= 0; i-- {
		if c := &m.console[i]; c.waiting && c.cmd == e.cmd {
			c.waiting, c.out = false, e.out
			if e.err != nil {
				c.out, c.err = e.err.Error(), true
			}
			break
		}
	}
	cmd, out := repeater.Redact(e.cmd, e.out)
	res := strings.SplitN(out, "\n", 2)[0]
	if e.err != nil {
		res = "error: " + e.err.Error()
	}
	m.st.Log(m.now, metrics.EvCmd, fmt.Sprintf("%s → %s", cmd, res))
}

// keyPrompt handles keys while the prompt or confirmation is open.
func (m Model) keyPrompt(k tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.mode == modeConfirm {
		switch k.String() {
		case "y", "Y":
			m.mode = modePrompt
			cmd := m.send(m.pending)
			m.pending = ""
			return m, cmd
		case "n", "N", "esc":
			m.console = append(m.console, consoleLine{at: m.now, cmd: m.pending, out: "cancelled — nothing sent"})
			m.mode, m.pending = modePrompt, ""
		}
		return m, nil
	}
	switch k.String() {
	case "esc":
		m.mode = modeNormal
		m.input.Blur()
		return m, nil
	case "enter":
		v := strings.TrimSpace(m.input.Value())
		if v == "" {
			return m, nil
		}
		m.history = append(m.history, v)
		m.histIdx = len(m.history)
		m.input.Reset()
		if repeater.IsReadOnly(v) {
			return m, m.send(v)
		}
		m.pending, m.mode = v, modeConfirm
		return m, nil
	case "up":
		if m.histIdx > 0 {
			m.histIdx--
			m.input.SetValue(m.history[m.histIdx])
			m.input.CursorEnd()
		}
		return m, nil
	case "down":
		if m.histIdx < len(m.history)-1 {
			m.histIdx++
			m.input.SetValue(m.history[m.histIdx])
		} else {
			m.histIdx = len(m.history)
			m.input.Reset()
		}
		m.input.CursorEnd()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m Model) helpBox() string {
	th := m.th()
	w := min(84, m.w-4)
	iw := w - 4
	var l []string
	for _, s := range help.Sections {
		l = append(l, "")
		body := lipgloss.NewStyle().Width(iw - 14).Render(s.Text)
		for i, bl := range strings.Split(body, "\n") {
			lab := strings.Repeat(" ", 14)
			if i == 0 {
				lab = fg(th.Title2).Bold(true).Render(fmt.Sprintf("%-14s", s.Title))
			}
			l = append(l, " "+lab+fg(th.Fg).Render(bl))
		}
	}
	l = append(l, "", fg(th.Dim).Italic(true).Render(" press h, ? or esc to close"))
	return box("What am I looking at?", strings.Join(l, "\n"), w, min(len(l)+2, m.h-2), th, th.Title1)
}

func (m Model) consoleBox(h int) string {
	th := m.th()
	w := m.w - 4
	var l []string
	if len(m.console) == 0 {
		l = append(l, fg(th.Dim).Italic(true).Render(" no commands yet — try: get tx, neighbors, clock"))
	}
	for _, c := range m.console {
		l = append(l, fg(th.Dim).Render(c.at.Format("15:04:05"))+" "+fg(th.Title1).Bold(true).Render("❯ "+c.cmd))
		switch {
		case c.waiting:
			l = append(l, fg(th.Dim).Italic(true).Render("   waiting for reply…"))
		case c.err:
			l = append(l, fg(th.Err).Render("   ✗ "+c.out))
		default:
			out := c.out
			if out == "" {
				out = "(no reply)"
			}
			for _, ol := range strings.Split(out, "\n") {
				l = append(l, fg(th.Good).Render("   "+ol))
			}
		}
	}
	if len(l) > h-2 {
		l = l[len(l)-(h-2):]
	}
	return box("Repeater console · "+m.info.Name, strings.Join(l, "\n"), w, h, th, th.Title1)
}

func (m Model) confirmBox() string {
	th := m.th()
	msg := fg(th.Warn).Bold(true).Render("⚠ ") + fg(th.Fg).Render("'") + fg(th.Warn).Bold(true).Render(m.pending) +
		fg(th.Fg).Render("' can change the repeater.")
	keys := fg(th.Fg).Render("Send it?  ") + fg(th.Good).Bold(true).Render("[y]") + fg(th.Dim).Render(" yes   ") +
		fg(th.Err).Bold(true).Render("[n]") + fg(th.Dim).Render(" no")
	w := min(m.w-4, max(lipgloss.Width(msg), lipgloss.Width(keys))+6)
	body := lipgloss.PlaceHorizontal(w-2, lipgloss.Center, msg) + "\n\n" + lipgloss.PlaceHorizontal(w-2, lipgloss.Center, keys)
	return box("Confirm", body, w, 5, th, th.Warn)
}

func (m Model) promptLine() string {
	th := m.th()
	hint := fg(th.Dim).Render(" enter send · tab complete · ↑↓ history · esc close ")
	in := m.input.View()
	return fit(" "+in+strings.Repeat(" ", max(1, m.w-1-lipgloss.Width(in)-lipgloss.Width(hint)))+hint, m.w)
}

// overlay composites the open dialogs over the dashboard.
func (m Model) overlay(base string) string {
	var layers []*lipgloss.Layer
	switch m.mode {
	case modeHelp:
		hb := m.helpBox()
		layers = append(layers, lipgloss.NewLayer(hb).
			X((m.w-lipgloss.Width(hb))/2).Y(max(1, (m.h-lipgloss.Height(hb))/2)).Z(1))
	case modePrompt, modeConfirm:
		ch := max(6, min(16, m.h/3))
		layers = append(layers, lipgloss.NewLayer(m.consoleBox(ch)).X(2).Y(m.h-1-ch).Z(1))
		if m.mode == modeConfirm {
			cb := m.confirmBox()
			layers = append(layers, lipgloss.NewLayer(cb).
				X((m.w-lipgloss.Width(cb))/2).Y((m.h-5)/2).Z(2))
		}
	default:
		return base
	}
	return lipgloss.NewCompositor(append([]*lipgloss.Layer{lipgloss.NewLayer(base)}, layers...)...).Render()
}
