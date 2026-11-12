package metrics

import (
	"fmt"
	"time"

	"meshtui/internal/repeater"
)

// EventKind classifies ticker events.
type EventKind int

const (
	EvRx EventKind = iota
	EvTx
	EvError
	EvReboot
	EvNeighbor
	EvSignal
	EvCmd
)

// Event is one line in the event ticker.
type Event struct {
	At   time.Time
	Kind EventKind
	Text string
}

const (
	historyLen = 900 // 30 min at 2 s
	maxEvents  = 200
	// NoSignalAfter is the number of consecutive failed polls that count as lost signal.
	NoSignalAfter = 3
)

type airPoint struct {
	t  time.Time
	tx int
}

const (
	dutyWindow    = 10 * time.Minute
	dutyMinWindow = time.Minute
)

// State accumulates samples.
type State struct {
	Latest                      repeater.Sample
	Noise, RSSI, SNR, BatteryV  *Ring
	RxPerMin, TxPerMin, DutyPct *Ring
	Events                      []Event
	NewRx, NewTx                int
	Failures                    int
	Neighbors                   []repeater.Neighbor
	// Pushed says which rings got a value from the last Apply.
	Pushed struct{ Signal, Rates, Duty bool }
	prev   *repeater.Sample
	air    []airPoint
	seen   map[string]bool
}

// NewState returns an empty State.
func NewState() *State {
	return &State{
		Noise: NewRing(historyLen), RSSI: NewRing(historyLen), SNR: NewRing(historyLen), BatteryV: NewRing(historyLen),
		RxPerMin: NewRing(historyLen), TxPerMin: NewRing(historyLen), DutyPct: NewRing(historyLen),
		seen: map[string]bool{},
	}
}

// Apply folds one sample into the state and returns the events it produced.
func (s *State) Apply(x repeater.Sample) []Event {
	var ev []Event
	add := func(k EventKind, f string, a ...any) {
		ev = append(ev, Event{At: x.At, Kind: k, Text: fmt.Sprintf(f, a...)})
	}
	s.NewRx, s.NewTx = 0, 0
	s.Pushed.Signal, s.Pushed.Rates, s.Pushed.Duty = false, false, false

	if x.Err != nil {
		s.Failures++
		if s.Failures == NoSignalAfter {
			add(EvSignal, "signal lost: %v", x.Err)
		}
		s.record(ev)
		return ev
	}
	if s.Failures >= NoSignalAfter {
		add(EvSignal, "signal restored")
	}
	s.Failures = 0
	s.Latest = x

	s.Noise.Push(float64(x.Radio.NoiseFloor))
	s.RSSI.Push(float64(x.Radio.LastRSSI))
	s.SNR.Push(x.Radio.LastSNR)
	s.BatteryV.Push(float64(x.Core.BatteryMV) / 1000)
	s.Pushed.Signal = true

	if p := s.prev; p != nil {
		dp, dc := x.Packets, x.Core
		rebooted := dp.Recv < p.Packets.Recv || dp.Sent < p.Packets.Sent || dc.UptimeSecs < p.Core.UptimeSecs ||
			x.Radio.TxAirSecs < p.Radio.TxAirSecs || dp.RecvErrors < p.Packets.RecvErrors
		dt := x.At.Sub(p.At).Seconds()
		switch {
		case rebooted:
			add(EvReboot, "repeater rebooted — counters reset")
			s.RxPerMin.Push(0)
			s.TxPerMin.Push(0)
			s.Pushed.Rates = true
			s.air = nil
		case dt > 0:
			s.NewRx = dp.Recv - p.Packets.Recv
			s.NewTx = dp.Sent - p.Packets.Sent
			s.RxPerMin.Push(float64(s.NewRx) * 60 / dt)
			s.TxPerMin.Push(float64(s.NewTx) * 60 / dt)
			s.Pushed.Rates = true
			if s.NewRx > 0 {
				add(EvRx, "rx +%d  rssi %d  snr %.1f", s.NewRx, x.Radio.LastRSSI, x.Radio.LastSNR)
			}
			if s.NewTx > 0 {
				add(EvTx, "relayed +%d (flood %d / direct %d)", s.NewTx,
					dp.FloodTx-p.Packets.FloodTx, dp.DirectTx-p.Packets.DirectTx)
			}
			if d := dp.RecvErrors - p.Packets.RecvErrors; d > 0 {
				add(EvError, "recv_errors +%d", d)
			}
		}
	}
	s.updateDuty(x)
	if x.HasNeighbors {
		for _, n := range x.Neighbors {
			if !s.seen[n.ID] {
				s.seen[n.ID] = true
				add(EvNeighbor, "new neighbor %s  snr %.1f dB", n.ID, n.SNR)
			}
		}
		s.Neighbors = x.Neighbors
	}
	cp := x
	s.prev = &cp
	s.record(ev)
	return ev
}

// updateDuty computes TX duty over a sliding window: tx_air_secs is whole
// seconds, so a per-poll delta would only ever read 0% or 50%.
func (s *State) updateDuty(x repeater.Sample) {
	s.air = append(s.air, airPoint{x.At, x.Radio.TxAirSecs})
	cut := 0
	for cut < len(s.air)-1 && x.At.Sub(s.air[cut].t) > dutyWindow {
		cut++
	}
	s.air = s.air[cut:]
	first := s.air[0]
	if span := x.At.Sub(first.t); span >= dutyMinWindow {
		s.DutyPct.Push(float64(x.Radio.TxAirSecs-first.tx) * 100 / span.Seconds())
		s.Pushed.Duty = true
	}
}

// Log adds a non-sample event (e.g. a manual command) to the ticker.
func (s *State) Log(at time.Time, k EventKind, text string) {
	s.record([]Event{{At: at, Kind: k, Text: text}})
}

func (s *State) record(ev []Event) {
	s.Events = append(s.Events, ev...)
	if over := len(s.Events) - maxEvents; over > 0 {
		s.Events = append([]Event(nil), s.Events[over:]...)
	}
}
