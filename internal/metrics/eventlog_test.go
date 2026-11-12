package metrics

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"meshtui/internal/repeater"
)

func TestEventLogLogsStateChangesOnly(t *testing.T) {
	var buf bytes.Buffer
	l := NewEventLog(log.New(&buf, "", 0))
	t0 := time.Now()
	s := repeater.Sample{At: t0, Core: repeater.Core{UptimeSecs: 100}, Packets: repeater.Packets{Recv: 10, Sent: 5}}
	l.SetInfo(repeater.Info{Name: "R1", Firmware: "v1.18.0"})
	l.Observe(s)
	s.At, s.Packets.Recv = t0.Add(2*time.Second), 12 // rx: not logged
	l.Observe(s)
	for i := 0; i < NoSignalAfter; i++ {
		l.Observe(repeater.Sample{At: t0.Add(time.Duration(4+i) * time.Second), Err: errors.New("timeout")})
	}
	s.At, s.Core.UptimeSecs, s.Packets.Recv = t0.Add(20*time.Second), 3, 0 // reboot
	l.Observe(s)
	got := buf.String()
	for _, want := range []string{"R1", "v1.18.0", "signal lost: timeout", "signal restored", "rebooted"} {
		if !strings.Contains(got, want) {
			t.Errorf("log missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "rx +") {
		t.Errorf("packet events must not be logged:\n%s", got)
	}
	if n := strings.Count(got, "signal lost"); n != 1 {
		t.Errorf("signal lost logged %d times, want 1", n)
	}
}

func TestEventLogInfoOnlyOnChange(t *testing.T) {
	var buf bytes.Buffer
	l := NewEventLog(log.New(&buf, "", 0))
	in := repeater.Info{Name: "R1", Firmware: "v1"}
	l.SetInfo(in)
	l.SetInfo(in)
	if n := strings.Count(buf.String(), "R1"); n != 1 {
		t.Errorf("info logged %d times, want 1:\n%s", n, buf.String())
	}
}
