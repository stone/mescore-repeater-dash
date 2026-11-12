package repeater

import (
	"io"
	"strings"
	"testing"
)

// fakeRepeater answers allowlisted commands like the firmware does.
type fakeRepeater struct {
	replies map[string]string
	ch      chan []byte
	closed  chan struct{}
}

func newFakeRepeater(replies map[string]string) *fakeRepeater {
	return &fakeRepeater{replies: replies, ch: make(chan []byte, 16), closed: make(chan struct{})}
}

func (f *fakeRepeater) Write(p []byte) (int, error) {
	cmd := strings.TrimSuffix(string(p), "\r")
	f.ch <- []byte(cmd + "\r\n  -> " + f.replies[cmd] + "\r\n")
	return len(p), nil
}

func (f *fakeRepeater) Read(p []byte) (int, error) {
	select {
	case b := <-f.ch:
		return copy(p, b), nil
	case <-f.closed:
		return 0, io.EOF
	}
}

func (f *fakeRepeater) Close() error { close(f.closed); return nil }

type eofPort struct{}

func (eofPort) Read([]byte) (int, error)    { return 0, io.EOF }
func (eofPort) Write(p []byte) (int, error) { return len(p), nil }
func (eofPort) Close() error                { return nil }

var goodReplies = map[string]string{
	"stats-core":    `{"battery_mv":4331,"uptime_secs":18637,"errors":0,"queue_len":0}`,
	"stats-radio":   `{"noise_floor":-111,"last_rssi":-106,"last_snr":4.25,"tx_air_secs":225,"rx_air_secs":235}`,
	"stats-packets": `{"recv":350,"sent":325,"flood_tx":292,"direct_tx":33,"flood_rx":298,"direct_rx":52,"recv_errors":14}`,
	"neighbors":     "-none-",
}

func TestSerialSourceReopensAfterEOF(t *testing.T) {
	opens := 0
	src := newSerialSource(func() (io.ReadWriteCloser, error) {
		opens++
		if opens == 1 {
			return eofPort{}, nil
		}
		return newFakeRepeater(goodReplies), nil
	})
	if s := src.Poll(false); s.Err == nil {
		t.Fatal("first poll on dead port should fail")
	}
	s := src.Poll(true)
	if s.Err != nil {
		t.Fatalf("second poll: %v", s.Err)
	}
	if opens != 2 {
		t.Fatalf("opens=%d want 2", opens)
	}
	if s.Core.BatteryMV != 4331 || s.Radio.LastSNR != 4.25 || s.Packets.RecvErrors != 14 {
		t.Fatalf("bad sample %+v", s)
	}
	if !s.HasNeighbors || len(s.Neighbors) != 0 {
		t.Fatalf("neighbors %+v %v", s.Neighbors, s.HasNeighbors)
	}
}

func TestDemoSourceProducesGrowingCounters(t *testing.T) {
	d := NewDemo(1)
	a := d.Poll(true)
	var b Sample
	for i := 0; i < 20; i++ {
		b = d.Poll(false)
	}
	if a.Err != nil || b.Err != nil || b.Packets.Recv < a.Packets.Recv || b.Core.BatteryMV < 3000 {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
	if in, err := d.Info(); err != nil || !strings.HasPrefix(in.Name, "SE-") {
		t.Fatalf("info %+v %v", in, err)
	}
}

func TestPollRejectsWrongPayload(t *testing.T) {
	r := map[string]string{}
	for k, v := range goodReplies {
		r[k] = v
	}
	r["stats-core"] = goodReplies["stats-radio"] // misattributed reply
	src := newSerialSource(func() (io.ReadWriteCloser, error) { return newFakeRepeater(r), nil })
	if s := src.Poll(false); s.Err == nil {
		t.Fatalf("want error for wrong payload, got %+v", s)
	}
}

func TestOpenSerialMissingDeviceDoesNotFail(t *testing.T) {
	src, err := OpenSerial("/dev/does-not-exist-meshtui", 115200)
	if err != nil || src == nil {
		t.Fatalf("OpenSerial: %v", err)
	}
	if s := src.Poll(false); s.Err == nil {
		t.Fatal("want error sample")
	}
}
