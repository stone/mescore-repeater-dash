package ui

import (
	"image/color"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

const waveLife = 1100 * time.Millisecond

type wave struct {
	born time.Time
	tx   bool // outward when true, inward when false
}

type cell struct {
	ch rune
	c  color.Color
}

// drawAntenna renders a w×h radio mast with animated rx/tx waves.
func drawAntenna(w, h int, now time.Time, waves []wave, th Theme, alive bool) string {
	if w < 7 || h < 4 {
		return ""
	}
	grid := make([][]cell, h)
	for y := range grid {
		grid[y] = make([]cell, w)
		for x := range grid[y] {
			grid[y][x] = cell{' ', nil}
		}
	}
	set := func(x, y int, ch rune, c color.Color) {
		if y >= 0 && y < h && x >= 0 && x < w {
			grid[y][x] = cell{ch, c}
		}
	}
	cx, tip := w/2, 1
	maxR := float64(cx - 1)

	// Mast.
	for y := tip + 1; y < h; y++ {
		k := (y - tip) / 2
		set(cx, y, '┃', th.Dim)
		if k > 0 {
			set(cx-k, y, '╱', th.Border)
			set(cx+k, y, '╲', th.Border)
			if (y-tip)%3 == 0 {
				for x := cx - k + 1; x < cx+k; x++ {
					if x != cx {
						set(x, y, '─', th.Border)
					}
				}
			}
		}
	}

	// Waves.
	active := false
	for _, wv := range waves {
		age := now.Sub(wv.born)
		if age < 0 || age > waveLife {
			continue
		}
		active = true
		f := float64(age) / float64(waveLife)
		r := 2 + f*(maxR-2)
		base := th.Tx
		if !wv.tx {
			r = maxR - f*(maxR-2)
			base = th.Rx
		}
		ri := int(math.Round(r))
		c := lipgloss.Blend1D(10, base, th.Bg)[min(int(f*10), 9)]
		set(cx-ri, tip, '(', c)
		set(cx+ri, tip, ')', c)
		if ri >= 2 {
			set(cx-ri+1, tip-1, '╱', c)
			set(cx+ri-1, tip-1, '╲', c)
			set(cx-ri+1, tip+1, '╲', c)
			set(cx+ri-1, tip+1, '╱', c)
		}
	}

	// Tip: bright while waves fly, slow heartbeat otherwise.
	tipC := th.Dim
	switch {
	case !alive:
		if now.UnixMilli()/500%2 == 0 {
			tipC = th.Err
		}
	case active:
		tipC = th.Warn
	default:
		p := (math.Sin(float64(now.UnixMilli())/400) + 1) / 2
		tipC = lipgloss.Blend1D(10, th.Dim, th.Good)[min(int(p*10), 9)]
	}
	set(cx, tip, '◆', tipC)

	var b strings.Builder
	for y, row := range grid {
		for _, c := range row {
			if c.c == nil {
				b.WriteRune(c.ch)
			} else {
				b.WriteString(fg(c.c).Render(string(c.ch)))
			}
		}
		if y < h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
