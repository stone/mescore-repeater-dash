package repeater

import (
	"testing"
	"time"
)

func TestParseInfo(t *testing.T) {
	got := ParseInfo("v1.18.0-81dbae5 (Build: 15-Oct-2026)", "> SE-KSD-SkareRep", "> 869.6179809,62.5,8,8")
	want := Info{Firmware: "v1.18.0-81dbae5", Name: "SE-KSD-SkareRep", FreqMHz: 869.6179809, BWkHz: 62.5, SF: 8, CR: 8}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestParseNeighbors(t *testing.T) {
	if n := ParseNeighbors("-none-"); len(n) != 0 {
		t.Fatalf("want empty, got %v", n)
	}
	n := ParseNeighbors("A1B2C3D4:120:20\nDEADBEEF:5:-8\ngarbage")
	want := []Neighbor{{ID: "A1B2C3D4", HeardAgo: 120 * time.Second, SNR: 5}, {ID: "DEADBEEF", HeardAgo: 5 * time.Second, SNR: -2}}
	if len(n) != 2 || n[0] != want[0] || n[1] != want[1] {
		t.Fatalf("got %+v want %+v", n, want)
	}
}
