package repeater

import (
	"context"
	"testing"
	"time"
)

type recorder struct {
	samples int
	info    *Info
}

func (r *recorder) Observe(Sample)  { r.samples++ }
func (r *recorder) SetInfo(in Info) { r.info = &in }

func TestTeeFeedsAllObservers(t *testing.T) {
	a, b := &recorder{}, &recorder{}
	src := Tee(NewDemo(1), a, b)
	src.Poll(false)
	if _, err := src.Info(); err != nil {
		t.Fatal(err)
	}
	for _, r := range []*recorder{a, b} {
		if r.samples != 1 || r.info == nil || r.info.Name != "SE-DEMO-Repeater" {
			t.Fatalf("observer got %+v", r)
		}
	}
	if out, err := src.Exec("get tx"); err != nil || out == "" {
		t.Fatalf("Exec must pass through: %q %v", out, err)
	}
}

func TestRunPollsUntilCancelled(t *testing.T) {
	r := &recorder{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { Run(ctx, Tee(NewDemo(1), r), time.Millisecond); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on cancel")
	}
	if r.samples < 2 || r.info == nil {
		t.Fatalf("got %+v", r)
	}
}
