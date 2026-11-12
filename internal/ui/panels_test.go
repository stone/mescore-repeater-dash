package ui

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestLocalTimeLabel(t *testing.T) {
	ts := time.Date(2026, 11, 11, 21, 35, 7, 0, time.Local)
	if got := localTimeLabel(0, float64(ts.Unix())); got != "21:35:07" {
		t.Fatalf("got %q", got)
	}
}

func TestMirroredBars(t *testing.T) {
	th := themes[0]
	got := ansi.Strip(mirrored([]float64{8, 0}, []float64{2, 0}, 2, 4, th))
	want := "█ \n█ \n▀▔\n  "
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

func TestMirroredPadsLeft(t *testing.T) {
	got := ansi.Strip(mirrored([]float64{8}, []float64{0}, 3, 2, themes[0]))
	if want := "  █\n▔▔▔"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
