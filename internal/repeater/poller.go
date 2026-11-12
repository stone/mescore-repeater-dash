package repeater

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"go.bug.st/serial"
)

type serialSource struct {
	mu     sync.Mutex // Poll and Info may run in concurrent tea.Cmds
	open   func() (io.ReadWriteCloser, error)
	port   io.ReadWriteCloser
	client *Client
}

func newSerialSource(open func() (io.ReadWriteCloser, error)) *serialSource {
	return &serialSource{open: open}
}

// OpenSerial returns a Source on a serial device. The port is (re)opened lazily,
// so a missing or unplugged device yields error samples instead of exiting.
func OpenSerial(dev string, baud int) (Source, error) {
	s := newSerialSource(func() (io.ReadWriteCloser, error) {
		// Leave DTR/RTS untouched: toggling them resets ESP32 boards.
		return serial.Open(dev, &serial.Mode{BaudRate: baud})
	})
	return s, nil
}

func (s *serialSource) ensure() error {
	if s.client != nil {
		return nil
	}
	p, err := s.open()
	if err != nil {
		return err
	}
	s.port, s.client = p, NewClient(p)
	return nil
}

func (s *serialSource) drop() {
	if s.port != nil {
		s.port.Close()
	}
	s.port, s.client = nil, nil
}

func (s *serialSource) query(cmd string) (string, error) {
	return s.send(cmd, (*Client).Query)
}

func (s *serialSource) Exec(cmd string) (string, error) {
	return s.send(cmd, (*Client).Exec)
}

func (s *serialSource) send(cmd string, f func(*Client, string) (string, error)) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(); err != nil {
		return "", err
	}
	out, err := f(s.client, cmd)
	if err != nil {
		s.drop()
	}
	return out, err
}

func (s *serialSource) Info() (Info, error) {
	var r [3]string
	for i, cmd := range []string{"ver", "get name", "get radio"} {
		v, err := s.query(cmd)
		if err != nil {
			return Info{}, err
		}
		r[i] = v
	}
	return ParseInfo(r[0], r[1], r[2]), nil
}

func (s *serialSource) Poll(withNeighbors bool) Sample {
	smp := Sample{At: time.Now()}
	for _, q := range []struct {
		cmd, key string
		dst      any
	}{{"stats-core", "uptime_secs", &smp.Core}, {"stats-radio", "noise_floor", &smp.Radio}, {"stats-packets", "recv", &smp.Packets}} {
		v, err := s.query(q.cmd)
		if err == nil {
			err = decodeStats(q.cmd, v, q.key, q.dst)
		}
		if err != nil {
			return Sample{At: smp.At, Err: err}
		}
	}
	if withNeighbors {
		if v, err := s.query("neighbors"); err == nil {
			smp.Neighbors, smp.HasNeighbors = ParseNeighbors(v), true
		}
	}
	return smp
}

func (s *serialSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drop()
	return nil
}

// decodeStats unmarshals v into dst, requiring key so a misattributed reply
// (another command's JSON) is rejected instead of decoding as all zeros.
func decodeStats(cmd, v, key string, dst any) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(v), &m); err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	if _, ok := m[key]; !ok {
		return fmt.Errorf("%s: reply missing %q", cmd, key)
	}
	return json.Unmarshal([]byte(v), dst)
}
