package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme is a color palette for the whole dashboard.
type Theme struct {
	Name                       string
	Bg, Panel, Fg, Dim, Border color.Color
	Title1, Title2             color.Color
	Rx, Tx, Warn, Err, Good    color.Color
}

func hex(s string) color.Color { return lipgloss.Color(s) }

var themes = []Theme{
	{
		Name: "Synthwave", Bg: hex("#120a1f"), Panel: hex("#1b1030"), Fg: hex("#ece6ff"), Dim: hex("#76659a"), Border: hex("#4a3470"),
		Title1: hex("#ff6ad5"), Title2: hex("#8be9fd"),
		Rx: hex("#2de2e6"), Tx: hex("#ff3cac"), Warn: hex("#ffd319"), Err: hex("#ff2a6d"), Good: hex("#3cffa0"),
	},
	{
		Name: "Phosphor", Bg: hex("#030a05"), Panel: hex("#07140b"), Fg: hex("#c8ffd4"), Dim: hex("#3f7a4c"), Border: hex("#1f4a2a"),
		Title1: hex("#00ff6a"), Title2: hex("#d4ff5a"),
		Rx: hex("#00ff6a"), Tx: hex("#a6ff00"), Warn: hex("#ffe14d"), Err: hex("#ff5a5a"), Good: hex("#00ff6a"),
	},
	{
		Name: "Monokai", Bg: hex("#272822"), Panel: hex("#3e3d32"), Fg: hex("#f8f8f2"), Dim: hex("#75715e"), Border: hex("#49483e"),
		Title1: hex("#fd971f"), Title2: hex("#66d9ef"),
		Rx: hex("#a6e22e"), Tx: hex("#ae81ff"), Warn: hex("#e6db74"), Err: hex("#f92672"), Good: hex("#a6e22e"),
	},
	{
		Name: "Nord", Bg: hex("#242933"), Panel: hex("#2e3440"), Fg: hex("#eceff4"), Dim: hex("#7b88a1"), Border: hex("#4c566a"),
		Title1: hex("#88c0d0"), Title2: hex("#b48ead"),
		Rx: hex("#8fbcbb"), Tx: hex("#d08770"), Warn: hex("#ebcb8b"), Err: hex("#bf616a"), Good: hex("#a3be8c"),
	},
}

func fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// gradient renders s with a horizontal color blend from a to b.
func gradient(s string, a, b color.Color, bold bool) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	cols := lipgloss.Blend1D(max(len(r), 2), a, b)
	out := ""
	for i, ch := range r {
		st := fg(cols[i])
		if bold {
			st = st.Bold(true)
		}
		out += st.Render(string(ch))
	}
	return out
}
