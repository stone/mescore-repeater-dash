package ui

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	tslc "github.com/NimbleMarkets/ntcharts/v2/linechart/timeserieslinechart"
	"github.com/charmbracelet/x/ansi"

	"meshtui/internal/metrics"
	"meshtui/internal/repeater"
)

// fit truncates or pads a styled line to exactly w cells.
func fit(s string, w int) string {
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "")
	}
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

// box draws a rounded panel of exactly w×h cells with the title in the top border.
func box(title, body string, w, h int, th Theme, accent color.Color) string {
	iw, ih := w-2, h-2
	bc := fg(th.Border)
	t := " " + fg(accent).Bold(true).Render(title) + " "
	if lipgloss.Width(t) > iw-1 {
		t = ansi.Truncate(t, max(0, iw-1), "")
	}
	top := bc.Render("╭─") + t + bc.Render(strings.Repeat("─", max(0, iw-1-lipgloss.Width(t)))+"╮")
	lines := strings.Split(body, "\n")
	var b strings.Builder
	b.WriteString(top)
	for i := 0; i < ih; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		b.WriteString("\n" + bc.Render("│") + fit(l, iw) + bc.Render("│"))
	}
	b.WriteString("\n" + bc.Render("╰"+strings.Repeat("─", iw)+"╯"))
	return b.String()
}

func minmax(vs ...[]float64) (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, v := range vs {
		for _, x := range v {
			lo, hi = math.Min(lo, x), math.Max(hi, x)
		}
	}
	return
}

func waiting(th Theme) string { return fg(th.Dim).Italic(true).Render(" waiting for data…") }

// localTimeLabel formats a unix-seconds axis value in local time.
func localTimeLabel(_ int, v float64) string { return time.Unix(int64(v), 0).Format("15:04:05") }

// signalChart plots noise floor and last RSSI over time.
func signalChart(w, h int, times, noise, rssi []float64, th Theme) string {
	if len(times) < 2 || w < 10 || h < 3 {
		return waiting(th)
	}
	lo, hi := minmax(noise, rssi)
	t0, t1 := time.Unix(int64(times[0]), 0), time.Unix(int64(times[len(times)-1]), 0)
	if !t1.After(t0) {
		t1 = t0.Add(time.Second)
	}
	ch := tslc.New(w, h,
		tslc.WithTimeRange(t0, t1),
		tslc.WithYRange(math.Floor(lo-3), math.Ceil(hi+3)),
		tslc.WithXLabelFormatter(localTimeLabel),
		tslc.WithAxesStyles(fg(th.Border), fg(th.Dim)),
		tslc.WithDataSetStyle("noise", fg(th.Warn)),
		tslc.WithDataSetStyle("rssi", fg(th.Rx)),
		tslc.WithXYSteps(3, 2),
	)
	for i, t := range times {
		ts := time.Unix(int64(t), 0)
		ch.PushDataSet("noise", tslc.TimePoint{Time: ts, Value: noise[i]})
		ch.PushDataSet("rssi", tslc.TimePoint{Time: ts, Value: rssi[i]})
	}
	ch.DrawBrailleAll()
	return ch.View()
}

var eighths = []rune(" ▁▂▃▄▅▆▇█")

// columns draws the last w values as vertical bars, each colored by its height.
func columns(vals []float64, w, h int, lo, hi float64, stops ...color.Color) string {
	if len(vals) > w {
		vals = vals[len(vals)-w:]
	}
	pad := w - len(vals)
	pal := lipgloss.Blend1D(16, stops...)
	rows := make([]strings.Builder, h)
	for i := 0; i < w; i++ {
		if i < pad {
			for y := range rows {
				rows[y].WriteByte(' ')
			}
			continue
		}
		f := math.Max(0, math.Min(1, (vals[i-pad]-lo)/(hi-lo)))
		n := int(math.Round(f * float64(h*8)))
		st := fg(pal[min(int(f*16), 15)])
		for y := 0; y < h; y++ {
			level := n - (h-1-y)*8
			ch := eighths[max(0, min(8, level))]
			if ch == ' ' {
				rows[y].WriteByte(' ')
			} else {
				rows[y].WriteString(st.Render(string(ch)))
			}
		}
	}
	out := make([]string, h)
	for y := range rows {
		out[y] = rows[y].String()
	}
	return strings.Join(out, "\n")
}

// packetsChart is a mirrored bar chart: rx/min up, tx/min down.
func packetsChart(w, h int, rx, tx []float64, th Theme) string {
	if len(rx) == 0 || w < 4 || h < 2 {
		return waiting(th)
	}
	return mirrored(rx, tx, w, h, th)
}

// mirrored draws rx in eighth-blocks above a center line and tx in half-blocks
// below it, newest at the right, scaled to the larger of the two.
func mirrored(rx, tx []float64, w, h int, th Theme) string {
	if len(rx) > w {
		rx, tx = rx[len(rx)-w:], tx[len(tx)-w:]
	}
	_, top := minmax(rx, tx)
	top = math.Max(top, 1)
	uh := h / 2
	lh := h - uh
	pad := w - len(rx)
	rxS, txS, axS := fg(th.Rx), fg(th.Tx), fg(th.Border)
	rows := make([]strings.Builder, h)
	for i := 0; i < w; i++ {
		r, t := 0.0, 0.0
		if i >= pad {
			r, t = rx[i-pad], tx[i-pad]
		}
		n := int(math.Round(r / top * float64(uh*8)))
		for y := 0; y < uh; y++ {
			lv := n - (uh-1-y)*8
			if ch := eighths[max(0, min(8, lv))]; ch == ' ' {
				rows[y].WriteByte(' ')
			} else {
				rows[y].WriteString(rxS.Render(string(ch)))
			}
		}
		n = int(math.Round(t / top * float64(lh*2)))
		for y := 0; y < lh; y++ {
			lv := n - y*2
			switch {
			case lv >= 2:
				rows[uh+y].WriteString(txS.Render("█"))
			case lv == 1:
				rows[uh+y].WriteString(txS.Render("▀"))
			case y == 0:
				rows[uh+y].WriteString(axS.Render("▔"))
			default:
				rows[uh+y].WriteByte(' ')
			}
		}
	}
	out := make([]string, h)
	for y := range rows {
		out[y] = rows[y].String()
	}
	return strings.Join(out, "\n")
}

func gauge(f float64, w int, th Theme) string {
	f = math.Max(0, math.Min(1, f))
	n := f * float64(w)
	full := int(n)
	pal := lipgloss.Blend1D(max(w, 2), th.Good, th.Warn, th.Err)
	var b strings.Builder
	for i := 0; i < w; i++ {
		switch {
		case i < full:
			b.WriteString(fg(pal[i]).Render("█"))
		case i == full && n-float64(full) > 0.5:
			b.WriteString(fg(pal[i]).Render("▌"))
		default:
			b.WriteString(fg(th.Border).Render("░"))
		}
	}
	return b.String()
}

func fmtDur(d time.Duration) string {
	s := int(d.Seconds())
	switch {
	case s >= 86400:
		return fmt.Sprintf("%dd%02dh", s/86400, s%86400/3600)
	case s >= 3600:
		return fmt.Sprintf("%dh%02dm", s/3600, s%3600/60)
	case s >= 60:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%ds", s)
}

const dutyLimit = 10.0 // % — EU 869.4–869.65 MHz sub-band limit

func statsPanel(m Model, iw int) string {
	th := m.th()
	x := m.st.Latest
	lab := func(s string) string { return fg(th.Dim).Render(fmt.Sprintf("%-10s", s)) }
	num := func(c color.Color, v any) string { return fg(c).Bold(true).Render(fmt.Sprint(v)) }
	gw := max(6, iw-10-9)
	var l []string
	if _, ok := m.st.DutyPct.Last(); ok {
		l = append(l, lab("Duty")+gauge(m.dutyPos/dutyLimit, gw, th)+fmt.Sprintf(" %5.2f%%", m.dutyPos))
	} else {
		l = append(l, lab("Duty")+fg(th.Dim).Italic(true).Render("warming up (needs 1 min)…"))
	}
	l = append(l, lab("Airtime")+fg(th.Tx).Render("⇡ "+fmtDur(time.Duration(x.Radio.TxAirSecs)*time.Second))+
		"  "+fg(th.Rx).Render("⇣ "+fmtDur(time.Duration(x.Radio.RxAirSecs)*time.Second)))
	l = append(l, lab("Flood")+fg(th.Rx).Render(fmt.Sprintf("⇣ %-6d", x.Packets.FloodRx))+fg(th.Tx).Render(fmt.Sprintf("⇡ %d", x.Packets.FloodTx)))
	l = append(l, lab("Direct")+fg(th.Rx).Render(fmt.Sprintf("⇣ %-6d", x.Packets.DirectRx))+fg(th.Tx).Render(fmt.Sprintf("⇡ %d", x.Packets.DirectTx)))
	errC := th.Fg
	if x.Packets.RecvErrors > 0 {
		errC = th.Warn
	}
	l = append(l, lab("Errors")+num(errC, x.Packets.RecvErrors)+fg(th.Dim).Render("  queue ")+num(th.Fg, x.Core.QueueLen)+
		fg(th.Dim).Render("  sys ")+num(th.Fg, x.Core.Errors))
	bv := m.st.BatteryV.Values()
	l = append(l, lab("Battery")+num(th.Good, fmt.Sprintf("%.2fV ", float64(x.Core.BatteryMV)/1000))+
		columns(bv, max(4, iw-17), 1, 3.3, 4.25, th.Err, th.Warn, th.Good))
	l = append(l, "")
	nb := m.st.Neighbors
	l = append(l, fg(th.Title2).Bold(true).Render(fmt.Sprintf("Neighbors (%d)", len(nb))))
	if len(nb) == 0 {
		l = append(l, fg(th.Dim).Italic(true).Render("  none heard yet — keep listening"))
	}
	for _, n := range nb {
		f := (n.SNR + 10) / 20
		l = append(l, fmt.Sprintf("  %s %s %s %s",
			fg(th.Fg).Render(n.ID), gauge(f, 8, Theme{Good: th.Err, Warn: th.Warn, Err: th.Good, Border: th.Border}),
			fg(th.Fg).Render(fmt.Sprintf("%+5.1fdB", n.SNR)), fg(th.Dim).Render(fmtDur(n.HeardAgo)+" ago")))
	}
	return strings.Join(l, "\n")
}

var evIcon = map[metrics.EventKind]string{
	metrics.EvRx: "⇣", metrics.EvTx: "⇡", metrics.EvError: "⚠", metrics.EvReboot: "⟳",
	metrics.EvNeighbor: "★", metrics.EvSignal: "ϟ", metrics.EvCmd: "⌘",
}

func (th Theme) evColor(e metrics.Event) color.Color {
	switch e.Kind {
	case metrics.EvRx:
		return th.Rx
	case metrics.EvTx:
		return th.Tx
	case metrics.EvError, metrics.EvReboot:
		return th.Warn
	case metrics.EvNeighbor:
		return th.Title2
	case metrics.EvCmd:
		return th.Title1
	}
	if strings.Contains(e.Text, "lost") {
		return th.Err
	}
	return th.Good
}

func ticker(ev []metrics.Event, n int, now time.Time, th Theme) string {
	if len(ev) == 0 {
		return fg(th.Dim).Italic(true).Render(" listening for packets…")
	}
	var l []string
	for i := len(ev) - 1; i >= 0 && len(l) < n; i-- {
		e := ev[i]
		c := th.evColor(e)
		txt := fg(th.Fg)
		age := now.Sub(e.At)
		switch {
		case age < 2*time.Second:
			txt = fg(c).Bold(true)
		case age > 5*time.Minute:
			txt = fg(th.Dim)
		}
		l = append(l, fg(th.Dim).Render(e.At.Format("15:04:05"))+" "+fg(c).Bold(true).Render(evIcon[e.Kind])+" "+txt.Render(e.Text))
	}
	return strings.Join(l, "\n")
}

func batteryIcon(mv int, th Theme) string {
	f := math.Max(0, math.Min(1, (float64(mv)-3300)/(4200-3300)))
	pal := lipgloss.Blend1D(5, th.Err, th.Warn, th.Good)
	n := int(math.Round(f * 5))
	return fg(th.Dim).Render("▕") + fg(pal[min(int(f*5), 4)]).Render(strings.Repeat("█", n)) +
		fg(th.Border).Render(strings.Repeat("░", 5-n)) + fg(th.Dim).Render("▏")
}

func radioLine(in repeater.Info) string {
	if in.FreqMHz == 0 {
		return ""
	}
	return fmt.Sprintf("%.3f MHz · SF%d · BW%g · CR%d", in.FreqMHz, in.SF, in.BWkHz, in.CR)
}
