// Package ui is the Bubble Tea dashboard.
package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/harmonica"

	"meshtui/internal/metrics"
	"meshtui/internal/repeater"
)

type (
	pollMsg    struct{ s repeater.Sample }
	pollDueMsg struct{}
	infoMsg    struct{ info repeater.Info }
	frameMsg   time.Time
)

const (
	neighborEvery = repeater.NeighborEvery
	fastFrame     = time.Second / 30
	idleFrame     = time.Second / 4
)

// Model is the dashboard state.
type Model struct {
	src      repeater.Source
	info     repeater.Info
	interval time.Duration
	st       *metrics.State
	times    *metrics.Ring

	w, h    int
	theme   int
	paused  bool
	polls   int
	now     time.Time
	lastOK  time.Time
	waves   []wave
	spring  harmonica.Spring
	dutyPos float64
	dutyVel float64
	shownRx float64
	shownTx float64

	mode    mode
	input   textinput.Model
	console []consoleLine
	history []string
	histIdx int
	pending string

	// Charts are rebuilt on data/size/theme change, not every frame.
	signalView, snrView, pktView string
}

// New builds the dashboard for src.
func New(src repeater.Source, info repeater.Info, interval time.Duration) Model {
	return Model{
		src: src, info: info, interval: interval,
		st: metrics.NewState(), times: metrics.NewRing(900),
		now: time.Now(), spring: harmonica.NewSpring(harmonica.FPS(30), 5, 0.6),
		input: newInput(themes[0]),
	}
}

func (m Model) th() Theme { return themes[m.theme] }

func (m Model) pollCmd() tea.Cmd {
	src, nb := m.src, m.polls%neighborEvery == 0
	return func() tea.Msg { return pollMsg{src.Poll(nb)} }
}

func frameAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return frameMsg(t) })
}

// Init starts polling and the animation clock.
func (m Model) Init() tea.Cmd { return tea.Batch(m.pollCmd(), frameAfter(fastFrame)) }

// Update handles input, poll results and animation frames.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.mode {
		case modePrompt, modeConfirm:
			return m.keyPrompt(msg)
		case modeHelp:
			switch msg.String() {
			case "h", "?", "esc", "q":
				m.mode = modeNormal
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "esc":
			return m, tea.Quit
		case "h", "?":
			m.mode = modeHelp
		case ":":
			m.mode, m.histIdx = modePrompt, len(m.history)
			return m, m.input.Focus()
		case "t":
			m.theme = (m.theme + 1) % len(themes)
			val := m.input.Value()
			m.input = newInput(m.th())
			m.input.SetWidth(max(10, m.w-60))
			m.input.SetValue(val)
			m.rebuild()
		case "p":
			m.paused = !m.paused
		}
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.input.SetWidth(max(10, m.w-60))
		m.rebuild()
	case pollMsg:
		m.polls++
		m.st.Apply(msg.s)
		if msg.s.Err == nil {
			m.lastOK = msg.s.At
			m.times.Push(float64(msg.s.At.Unix()))
			now := time.Now()
			for i := 0; i < min(m.st.NewRx, 3); i++ {
				m.waves = append(m.waves, wave{born: now.Add(time.Duration(i) * 220 * time.Millisecond)})
			}
			for i := 0; i < min(m.st.NewTx, 3); i++ {
				m.waves = append(m.waves, wave{born: now.Add(time.Duration(i)*220*time.Millisecond + 500*time.Millisecond), tx: true})
			}
			m.rebuild()
		}
		next := tea.Tick(m.interval, func(time.Time) tea.Msg { return pollDueMsg{} })
		if msg.s.Err == nil && m.info.Firmware == "" {
			src := m.src
			return m, tea.Batch(next, func() tea.Msg {
				if in, err := src.Info(); err == nil {
					return infoMsg{in}
				}
				return nil
			})
		}
		return m, next
	case infoMsg:
		m.info = msg.info
	case execMsg:
		m.gotExec(msg)
	case pollDueMsg:
		if m.paused {
			return m, tea.Tick(m.interval, func(time.Time) tea.Msg { return pollDueMsg{} })
		}
		return m, m.pollCmd()
	case frameMsg:
		m.now = time.Time(msg)
		return m, frameAfter(m.animate())
	}
	return m, nil
}

// animate advances springs/counters/waves and returns the next frame delay.
func (m *Model) animate() time.Duration {
	target, _ := m.st.DutyPct.Last()
	m.dutyPos, m.dutyVel = m.spring.Update(m.dutyPos, m.dutyVel, target)
	busy := math.Abs(m.dutyPos-target) > 0.005
	step := func(shown *float64, real int) {
		if d := float64(real) - *shown; d != 0 {
			busy = true
			*shown += math.Copysign(math.Max(1, math.Abs(d)/6), d)
			if math.Abs(float64(real)-*shown) < 1 {
				*shown = float64(real)
			}
		}
	}
	step(&m.shownRx, m.st.Latest.Packets.Recv)
	step(&m.shownTx, m.st.Latest.Packets.Sent)
	live := m.waves[:0]
	for _, w := range m.waves {
		if m.now.Sub(w.born) <= waveLife {
			live = append(live, w)
		}
	}
	m.waves = live
	if busy || len(m.waves) > 0 {
		return fastFrame
	}
	return idleFrame
}

type layout struct {
	topH, midH, botH int
	antW, snrW, sigW int
	statW, tickW     int
}

func (m Model) layout() layout {
	body := m.h - 2
	l := layout{topH: max(7, body*45/100), midH: max(5, body*24/100)}
	l.botH = body - l.topH - l.midH
	for l.botH < 9 && l.topH > 7 {
		l.topH--
		l.botH++
	}
	for l.botH < 9 && l.midH > 5 {
		l.midH--
		l.botH++
	}
	l.antW = max(16, min(30, m.w/4))
	l.snrW = (m.w - l.antW) * 32 / 100
	l.sigW = m.w - l.antW - l.snrW
	l.statW = max(38, min(64, m.w*42/100))
	l.tickW = m.w - l.statW
	return l
}

func (m *Model) rebuild() {
	if m.w < 60 || m.h < 20 {
		return
	}
	l, th := m.layout(), m.th()
	m.signalView = signalChart(l.sigW-2, l.topH-2, m.times.Values(), m.st.Noise.Values(), m.st.RSSI.Values(), th)
	if len(m.st.SNR.Values()) == 0 {
		m.snrView = waiting(th)
	} else {
		m.snrView = columns(m.st.SNR.Values(), l.snrW-2, l.topH-2, -20, 15, th.Err, th.Warn, th.Good)
	}
	m.pktView = packetsChart(m.w-2, l.midH-2, m.st.RxPerMin.Values(), m.st.TxPerMin.Values(), th)
}

func (m Model) header() string {
	th := m.th()
	if m.st.Failures >= metrics.NoSignalAfter {
		bg := th.Err
		if m.now.UnixMilli()/600%2 == 0 {
			bg = lipgloss.Darken(th.Err, 0.35)
		}
		msg := fmt.Sprintf(" ϟ NO SIGNAL — %d failed polls, retrying…  last seen %s ", m.st.Failures, ago(m.now, m.lastOK))
		return lipgloss.NewStyle().Background(bg).Foreground(th.Bg).Bold(true).Width(m.w).Render(fit(msg, m.w))
	}
	dot := fg(th.Good).Render("●")
	state := fg(th.Good).Bold(true).Render("LIVE")
	if m.now.UnixMilli()/700%2 == 0 {
		dot = fg(lipgloss.Darken(th.Good, 0.5)).Render("●")
	}
	if m.paused {
		dot, state = fg(th.Warn).Render("‖"), fg(th.Warn).Bold(true).Render("PAUSED")
	}
	sep := fg(th.Border).Render("  │  ")
	left := " " + dot + " " + state + sep + gradient(m.info.Name, th.Title1, th.Title2, true)
	opt := []string{fg(th.Dim).Render(m.info.Firmware), fg(th.Title2).Render(radioLine(m.info))}
	c := m.st.Latest.Core
	right := fg(th.Dim).Render("up ") + fg(th.Fg).Render(fmtDur(time.Duration(c.UptimeSecs)*time.Second)) + sep +
		batteryIcon(c.BatteryMV, th) + " " + fg(th.Fg).Render(fmt.Sprintf("%.2fV", float64(c.BatteryMV)/1000)) + sep +
		fg(th.Fg).Bold(true).Render(m.now.Format("15:04:05")) + " "
	for _, o := range opt {
		if o != "" && lipgloss.Width(left+sep+o+right)+1 <= m.w {
			left += sep + o
		}
	}
	gap := max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right))
	return fit(left+strings.Repeat(" ", gap)+right, m.w)
}

func ago(now, t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return fmtDur(now.Sub(t)) + " ago"
}

func (m Model) footer() string {
	th := m.th()
	k := func(key, what string) string {
		return fg(th.Title1).Bold(true).Render(key) + fg(th.Dim).Render(" "+what)
	}
	left := " " + k("h", "help") + "   " + k(":", "command") + "   " + k("t", "theme · "+th.Name) + "   " + k("p", "pause") + "   " + k("q", "quit")
	right := fg(th.Dim).Render(fmt.Sprintf("polls %d · updated %s ", m.polls, ago(m.now, m.lastOK)))
	return fit(left+strings.Repeat(" ", max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right)))+right, m.w)
}

func (m Model) antennaPanel(w, h int) string {
	th := m.th()
	iw, ih := w-2, h-2
	art := drawAntenna(iw, max(0, ih-3), m.now, m.waves, th, m.st.Failures < metrics.NoSignalAfter)
	rx := fg(th.Rx).Bold(true).Render(fmt.Sprintf("⇣ rx %d", int(m.shownRx)))
	tx := fg(th.Tx).Bold(true).Render(fmt.Sprintf("⇡ tx %d", int(m.shownTx)))
	r := m.st.Latest.Radio
	last := fg(th.Dim).Render(fmt.Sprintf("%d dBm · %.1f dB", r.LastRSSI, r.LastSNR))
	center := func(s string) string { return lipgloss.PlaceHorizontal(iw, lipgloss.Center, s) }
	body := art + "\n" + center(rx+"  "+tx) + "\n" + center(last)
	return box("Radio", body, w, h, th, th.Title1)
}

// View renders the dashboard.
func (m Model) View() tea.View {
	th := m.th()
	var content string
	if m.w < 60 || m.h < 20 {
		content = lipgloss.Place(max(m.w, 1), max(m.h, 1), lipgloss.Center, lipgloss.Center,
			fg(th.Warn).Render("terminal too small — need 60×20"))
	} else {
		l := m.layout()
		snr := m.st.Latest.Radio.LastSNR
		noise, rssi := m.st.Latest.Radio.NoiseFloor, m.st.Latest.Radio.LastRSSI
		sigTitle := fmt.Sprintf("Noise %d · RSSI %d dBm", noise, rssi)
		top := lipgloss.JoinHorizontal(lipgloss.Top,
			m.antennaPanel(l.antW, l.topH),
			box(sigTitle, m.signalView, l.sigW, l.topH, th, th.Rx),
			box(fmt.Sprintf("SNR %.1f dB", snr), m.snrView, l.snrW, l.topH, th, th.Good),
		)
		rxm, _ := m.st.RxPerMin.Last()
		txm, _ := m.st.TxPerMin.Last()
		pktTitle := fmt.Sprintf("Packets / min  ▲ rx %.0f  ▼ tx %.0f", rxm, txm)
		mid := box(pktTitle, m.pktView, m.w, l.midH, th, th.Tx)
		bot := lipgloss.JoinHorizontal(lipgloss.Top,
			box("Stats", statsPanel(m, l.statW-2), l.statW, l.botH, th, th.Title2),
			box("Live feed", ticker(m.st.Events, l.botH-2, m.now, th), l.tickW, l.botH, th, th.Warn),
		)
		foot := m.footer()
		if m.mode == modePrompt || m.mode == modeConfirm {
			foot = m.promptLine()
		}
		content = m.overlay(lipgloss.JoinVertical(lipgloss.Left, m.header(), top, mid, bot, foot))
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.BackgroundColor = th.Bg
	v.WindowTitle = "meshtui · " + m.info.Name
	return v
}
