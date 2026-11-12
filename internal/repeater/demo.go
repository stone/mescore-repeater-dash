package repeater

import (
	"math"
	"math/rand"
	"strings"
	"time"
)

type demo struct {
	r     *rand.Rand
	s     Sample
	tick  int
	nbIDs []string
}

// NewDemo returns a simulated repeater for UI work without hardware.
func NewDemo(seed int64) Source {
	d := &demo{r: rand.New(rand.NewSource(seed)), nbIDs: []string{"A1B2C3D4", "5EEDBEEF", "C0FFEE42", "77AB12CD"}}
	d.s.Core = Core{BatteryMV: 4200, UptimeSecs: 3600}
	d.s.Radio = Radio{NoiseFloor: -111, LastRSSI: -100, LastSNR: 4}
	d.s.Packets = Packets{Recv: 300, Sent: 280}
	return d
}

func (d *demo) Info() (Info, error) {
	return Info{Firmware: "v1.18.0-demo", Name: "SE-DEMO-Repeater", FreqMHz: 869.618, BWkHz: 62.5, SF: 8, CR: 8}, nil
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func (d *demo) Poll(withNeighbors bool) Sample {
	d.tick++
	r, s := d.r, &d.s
	s.At = time.Now()
	s.Core.UptimeSecs += 2
	s.Core.BatteryMV = int(4200 + 100*math.Sin(float64(d.tick)/90) + r.Float64()*10)
	s.Radio.NoiseFloor = int(clamp(float64(s.Radio.NoiseFloor)+float64(r.Intn(3)-1), -118, -100))
	// Bursty traffic: quiet most ticks, occasional bursts.
	rx := 0
	if r.Float64() < 0.45 {
		rx = 1 + r.Intn(4)
	}
	if rx > 0 {
		s.Radio.LastRSSI = int(clamp(float64(s.Radio.LastRSSI)+r.NormFloat64()*8, -125, -55))
		s.Radio.LastSNR = clamp(s.Radio.LastSNR+r.NormFloat64()*2, -12, 12)
		s.Radio.RxAirSecs += r.Intn(2)
		tx := r.Intn(rx + 1)
		s.Packets.Recv += rx
		s.Packets.Sent += tx
		fl := r.Intn(rx + 1)
		s.Packets.FloodRx += fl
		s.Packets.DirectRx += rx - fl
		ft := r.Intn(tx + 1)
		s.Packets.FloodTx += ft
		s.Packets.DirectTx += tx - ft
		if tx > 0 && r.Float64() < 0.5 {
			s.Radio.TxAirSecs++
		}
	}
	if r.Float64() < 0.03 {
		s.Packets.RecvErrors++
	}
	out := *s
	out.Neighbors, out.HasNeighbors = nil, false
	if withNeighbors {
		n := 1 + (d.tick/40)%len(d.nbIDs)
		for i := 0; i < n; i++ {
			out.Neighbors = append(out.Neighbors, Neighbor{ID: d.nbIDs[i], HeardAgo: time.Duration(r.Intn(900)) * time.Second, SNR: clamp(r.NormFloat64()*5, -12, 12)})
		}
		out.HasNeighbors = true
	}
	return out
}

func (d *demo) Exec(cmd string) (string, error) {
	cmd = strings.TrimSpace(cmd)
	switch {
	case cmd == "ver":
		return "v1.18.0-demo (Build: demo)", nil
	case strings.HasPrefix(cmd, "get "):
		return "> demo-" + strings.TrimPrefix(cmd, "get "), nil
	}
	return "OK (demo mode: nothing was sent)", nil
}

func (d *demo) Close() error { return nil }
