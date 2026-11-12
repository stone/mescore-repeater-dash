package metrics

import (
	"log"

	"meshtui/internal/repeater"
)

// EventLog is a repeater.Observer that writes state changes (identity, signal
// lost/restored, reboots) to a logger. Per-packet events are not logged.
type EventLog struct {
	l    *log.Logger
	st   *State
	info repeater.Info
}

// NewEventLog returns an EventLog writing to l.
func NewEventLog(l *log.Logger) *EventLog { return &EventLog{l: l, st: NewState()} }

// SetInfo logs the repeater identity when it changes.
func (e *EventLog) SetInfo(in repeater.Info) {
	if in == e.info {
		return
	}
	e.info = in
	e.l.Printf("repeater %s firmware %s, %.3f MHz BW%g SF%d CR%d", in.Name, in.Firmware, in.FreqMHz, in.BWkHz, in.SF, in.CR)
}

// Observe logs signal and reboot events from one poll.
func (e *EventLog) Observe(x repeater.Sample) {
	for _, ev := range e.st.Apply(x) {
		if ev.Kind == EvSignal || ev.Kind == EvReboot {
			e.l.Print(ev.Text)
		}
	}
}
