package repeater

import (
	"strconv"
	"strings"
	"time"
)

func trimPrompt(s string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), ">"))
}

// ParseInfo builds Info from the replies to `ver`, `get name` and `get radio`.
func ParseInfo(ver, name, radio string) Info {
	var in Info
	if f := strings.Fields(trimPrompt(ver)); len(f) > 0 {
		in.Firmware = f[0]
	}
	in.Name = trimPrompt(name)
	p := strings.Split(trimPrompt(radio), ",")
	if len(p) == 4 {
		in.FreqMHz, _ = strconv.ParseFloat(p[0], 64)
		in.BWkHz, _ = strconv.ParseFloat(p[1], 64)
		in.SF, _ = strconv.Atoi(p[2])
		in.CR, _ = strconv.Atoi(p[3])
	}
	return in
}

// ParseNeighbors parses `ID:secs_ago:snr*4` lines, skipping malformed ones.
func ParseNeighbors(payload string) []Neighbor {
	out := []Neighbor{}
	for _, line := range strings.Split(payload, "\n") {
		p := strings.Split(strings.TrimSpace(line), ":")
		if len(p) != 3 || p[0] == "" {
			continue
		}
		secs, err1 := strconv.Atoi(p[1])
		snr, err2 := strconv.Atoi(p[2])
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, Neighbor{ID: p[0], HeardAgo: time.Duration(secs) * time.Second, SNR: float64(snr) / 4})
	}
	return out
}
