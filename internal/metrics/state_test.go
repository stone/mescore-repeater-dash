package metrics

import (
	"errors"
	"strings"
	"testing"
	"time"

	"meshtui/internal/repeater"
)

var t0 = time.Date(2026, 11, 11, 21, 0, 0, 0, time.UTC)

func sample(at time.Duration, recv, sent, txAir, errs int) repeater.Sample {
	return repeater.Sample{
		At:      t0.Add(at),
		Core:    repeater.Core{BatteryMV: 4331},
		Radio:   repeater.Radio{NoiseFloor: -111, LastRSSI: -106, LastSNR: 4.25, TxAirSecs: txAir},
		Packets: repeater.Packets{Recv: recv, Sent: sent, RecvErrors: errs},
	}
}

func hasKind(ev []Event, k EventKind) (Event, bool) {
	for _, e := range ev {
		if e.Kind == k {
			return e, true
		}
	}
	return Event{}, false
}

func last(t *testing.T, r *Ring) float64 {
	t.Helper()
	v, ok := r.Last()
	if !ok {
		t.Fatal("ring empty")
	}
	return v
}

func TestRates(t *testing.T) {
	s := NewState()
	s.Apply(sample(0, 350, 325, 225, 14))
	s.Apply(sample(2*time.Second, 352, 326, 226, 14))
	if v := last(t, s.RxPerMin); v != 60 {
		t.Fatalf("rx/min %v", v)
	}
	if v := last(t, s.TxPerMin); v != 30 {
		t.Fatalf("tx/min %v", v)
	}
	if s.NewRx != 2 || s.NewTx != 1 {
		t.Fatalf("new rx/tx %d %d", s.NewRx, s.NewTx)
	}
}

func TestRebootResetsBaseline(t *testing.T) {
	s := NewState()
	s.Apply(sample(0, 350, 325, 225, 14))
	ev := s.Apply(sample(2*time.Second, 3, 1, 0, 0))
	if _, ok := hasKind(ev, EvReboot); !ok {
		t.Fatalf("no reboot event in %+v", ev)
	}
	for _, r := range []*Ring{s.RxPerMin, s.TxPerMin, s.DutyPct} {
		for _, v := range r.Values() {
			if v < 0 {
				t.Fatalf("negative rate %v", v)
			}
		}
	}
	if s.NewRx != 0 {
		t.Fatalf("NewRx %d", s.NewRx)
	}
}

func TestErrorsEvent(t *testing.T) {
	s := NewState()
	s.Apply(sample(0, 350, 325, 225, 14))
	ev := s.Apply(sample(2*time.Second, 350, 325, 225, 15))
	e, ok := hasKind(ev, EvError)
	if !ok || !strings.Contains(e.Text, "+1") {
		t.Fatalf("events %+v", ev)
	}
}

func TestFailuresAndSignal(t *testing.T) {
	s := NewState()
	s.Apply(sample(0, 350, 325, 225, 14))
	n := len(s.Noise.Values())
	for i := 0; i < 3; i++ {
		s.Apply(repeater.Sample{At: t0, Err: errors.New("timeout")})
	}
	if s.Failures != 3 || len(s.Noise.Values()) != n {
		t.Fatalf("failures %d noise len %d", s.Failures, len(s.Noise.Values()))
	}
	ev := s.Apply(sample(8*time.Second, 351, 325, 225, 14))
	e, ok := hasKind(ev, EvSignal)
	if s.Failures != 0 || !ok || !strings.Contains(e.Text, "signal restored") {
		t.Fatalf("failures %d events %+v", s.Failures, ev)
	}
}

func TestNeighborEvent(t *testing.T) {
	s := NewState()
	a := sample(0, 1, 1, 0, 0)
	a.HasNeighbors = true
	s.Apply(a)
	b := sample(2*time.Second, 1, 1, 0, 0)
	b.HasNeighbors, b.Neighbors = true, []repeater.Neighbor{{ID: "A1B2C3D4", SNR: 5}}
	ev := s.Apply(b)
	e, ok := hasKind(ev, EvNeighbor)
	if !ok || !strings.Contains(e.Text, "A1B2C3D4") {
		t.Fatalf("events %+v", ev)
	}
	if len(s.Neighbors) != 1 {
		t.Fatalf("neighbors %+v", s.Neighbors)
	}
}

func TestRingWraps(t *testing.T) {
	r := NewRing(3)
	for i := 1; i <= 5; i++ {
		r.Push(float64(i))
	}
	v := r.Values()
	if len(v) != 3 || v[0] != 3 || v[1] != 4 || v[2] != 5 {
		t.Fatalf("got %v", v)
	}
}

func TestDutyWindow(t *testing.T) {
	s := NewState()
	// 1 s of airtime every 100 s (50 polls at 2 s) = 1% duty.
	for i := 0; i <= 300; i++ {
		s.Apply(sample(time.Duration(i)*2*time.Second, 350, 325, 225+i/50, 14))
		if i < 29 {
			if _, ok := s.DutyPct.Last(); ok {
				t.Fatalf("poll %d: duty reported before 60 s window", i)
			}
		}
	}
	if v := last(t, s.DutyPct); v < 0.6 || v > 1.4 {
		t.Fatalf("duty %v, want ~1%%", v)
	}
	for _, v := range s.DutyPct.Values() {
		if v > 4 {
			t.Fatalf("duty spike %v", v)
		}
	}
}

func TestLogEvent(t *testing.T) {
	s := NewState()
	s.Log(t0, EvCmd, "get tx → 22")
	if len(s.Events) != 1 || s.Events[0].Kind != EvCmd || s.Events[0].Text != "get tx → 22" {
		t.Fatalf("events %+v", s.Events)
	}
}

func TestPushedFlags(t *testing.T) {
	s := NewState()
	s.Apply(sample(0, 1, 1, 0, 0))
	if s.Pushed.Signal != true || s.Pushed.Rates || s.Pushed.Duty {
		t.Fatalf("first sample: %+v", s.Pushed)
	}
	s.Apply(sample(2*time.Second, 2, 1, 0, 0))
	if !s.Pushed.Rates || s.Pushed.Duty {
		t.Fatalf("second sample: %+v", s.Pushed)
	}
	s.Apply(sample(62*time.Second, 3, 1, 0, 0))
	if !s.Pushed.Duty {
		t.Fatalf("after 60 s: %+v", s.Pushed)
	}
	s.Apply(repeater.Sample{At: t0, Err: errors.New("x")})
	if s.Pushed.Signal || s.Pushed.Rates || s.Pushed.Duty {
		t.Fatalf("error sample: %+v", s.Pushed)
	}
}
