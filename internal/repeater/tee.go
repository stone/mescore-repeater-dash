package repeater

import (
	"context"
	"time"
)

// Observer receives every poll result and identity read from a Source.
type Observer interface {
	Observe(Sample)
	SetInfo(Info)
}

type tee struct {
	Source
	obs []Observer
}

// Tee returns src with every Poll and successful Info fed to obs.
func Tee(src Source, obs ...Observer) Source { return tee{src, obs} }

func (t tee) Poll(nb bool) Sample {
	s := t.Source.Poll(nb)
	for _, o := range t.obs {
		o.Observe(s)
	}
	return s
}

func (t tee) Info() (Info, error) {
	in, err := t.Source.Info()
	if err == nil {
		for _, o := range t.obs {
			o.SetInfo(in)
		}
	}
	return in, err
}

// NeighborEvery is how often (in polls) neighbors are queried.
const NeighborEvery = 15

// Run polls src every interval until ctx is done (headless mode). It
// queries neighbors every NeighborEvery polls and retries Info until it succeeds.
func Run(ctx context.Context, src Source, interval time.Duration) {
	haveInfo := false
	for n := 0; ; n++ {
		if !haveInfo {
			_, err := src.Info()
			haveInfo = err == nil
		}
		src.Poll(n%NeighborEvery == 0)
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
