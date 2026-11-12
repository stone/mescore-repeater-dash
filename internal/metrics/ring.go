// Package metrics turns repeater samples into histories, rates and events.
package metrics

// Ring is a fixed-capacity FIFO of float64 values.
type Ring struct {
	buf   []float64
	start int
	n     int
}

// NewRing returns an empty ring holding at most n values.
func NewRing(n int) *Ring { return &Ring{buf: make([]float64, n)} }

// Push appends v, dropping the oldest value when full.
func (r *Ring) Push(v float64) {
	if r.n < len(r.buf) {
		r.buf[(r.start+r.n)%len(r.buf)] = v
		r.n++
		return
	}
	r.buf[r.start] = v
	r.start = (r.start + 1) % len(r.buf)
}

// Values returns the contents oldest first.
func (r *Ring) Values() []float64 {
	out := make([]float64, r.n)
	for i := range out {
		out[i] = r.buf[(r.start+i)%len(r.buf)]
	}
	return out
}

// Last returns the newest value.
func (r *Ring) Last() (float64, bool) {
	if r.n == 0 {
		return 0, false
	}
	return r.buf[(r.start+r.n-1)%len(r.buf)], true
}
