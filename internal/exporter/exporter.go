// Package exporter exposes repeater samples as Prometheus metrics.
package exporter

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"meshtui/internal/metrics"
	"meshtui/internal/repeater"
)

// Exporter caches the latest sample and serves it as a prometheus.Collector.
type Exporter struct {
	mu        sync.Mutex
	info      *repeater.Info
	last      *repeater.Sample
	neighbors []repeater.Neighbor
	hasNb     bool
	failures  int // consecutive
	failTotal int
	reg       *prometheus.Registry
	d         map[string]*prometheus.Desc
}

// New returns an Exporter with its own registry.
func New() *Exporter {
	e := &Exporter{d: map[string]*prometheus.Desc{}}
	add := func(name, help string, labels ...string) {
		e.d[name] = prometheus.NewDesc("meshcore_"+name, help, labels, nil)
	}
	add("up", "1 if the repeater answered recently (fewer than 3 consecutive failed polls).")
	add("info", "Repeater identity and radio settings.", "name", "firmware", "freq_mhz", "bw_khz", "sf", "cr")
	add("poll_failures_total", "Failed stats polls since start.")
	add("last_success_timestamp_seconds", "Unix time of the last successful poll.")
	add("battery_volts", "Battery voltage.")
	add("uptime_seconds", "Repeater uptime.")
	add("queue_length", "Outbound packet queue length.")
	add("core_errors", "Repeater core error counter.")
	add("noise_floor_dbm", "Radio noise floor.")
	add("last_rssi_dbm", "RSSI of the last received packet.")
	add("last_snr_db", "SNR of the last received packet.")
	add("tx_air_seconds_total", "Total transmit airtime.")
	add("rx_air_seconds_total", "Total receive airtime.")
	add("packets_received_total", "Packets received.")
	add("packets_sent_total", "Packets sent.")
	add("packets_total", "Packets by direction and route type.", "direction", "route")
	add("receive_errors_total", "Packets that failed to decode.")
	add("neighbors", "Number of neighbors heard directly.")
	add("neighbor_snr_db", "SNR of a neighbor.", "id")
	add("neighbor_last_heard_seconds", "Seconds since a neighbor was heard, as of the last neighbors query.", "id")
	e.reg = prometheus.NewRegistry()
	e.reg.MustRegister(e)
	return e
}

// Handler serves the metrics in Prometheus text format.
func (e *Exporter) Handler() http.Handler { return promhttp.HandlerFor(e.reg, promhttp.HandlerOpts{}) }

// SetInfo records the repeater identity.
func (e *Exporter) SetInfo(in repeater.Info) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.info = &in
}

// Observe records one poll result.
func (e *Exporter) Observe(s repeater.Sample) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s.Err != nil {
		e.failures++
		e.failTotal++
		return
	}
	e.failures = 0
	e.last = &s
	if s.HasNeighbors {
		e.neighbors, e.hasNb = s.Neighbors, true
	}
}

// Describe sends nothing: the metric set varies (neighbors), so this is an unchecked collector.
func (e *Exporter) Describe(chan<- *prometheus.Desc) {}

// Collect emits the cached values.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mu.Lock()
	defer e.mu.Unlock()
	g := func(name string, v float64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(e.d[name], prometheus.GaugeValue, v, labels...)
	}
	c := func(name string, v float64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(e.d[name], prometheus.CounterValue, v, labels...)
	}
	up := e.last != nil && e.failures < metrics.NoSignalAfter
	g("up", b2f(up))
	c("poll_failures_total", float64(e.failTotal))
	if in := e.info; in != nil {
		f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
		g("info", 1, in.Name, in.Firmware, f(in.FreqMHz), f(in.BWkHz), strconv.Itoa(in.SF), strconv.Itoa(in.CR))
	}
	if e.last != nil {
		g("last_success_timestamp_seconds", float64(e.last.At.UnixNano())/1e9)
	}
	if !up {
		return // don't export stale readings while the radio is silent
	}
	s := e.last
	g("battery_volts", float64(s.Core.BatteryMV)/1000)
	g("uptime_seconds", float64(s.Core.UptimeSecs))
	g("queue_length", float64(s.Core.QueueLen))
	g("core_errors", float64(s.Core.Errors))
	g("noise_floor_dbm", float64(s.Radio.NoiseFloor))
	g("last_rssi_dbm", float64(s.Radio.LastRSSI))
	g("last_snr_db", s.Radio.LastSNR)
	c("tx_air_seconds_total", float64(s.Radio.TxAirSecs))
	c("rx_air_seconds_total", float64(s.Radio.RxAirSecs))
	p := s.Packets
	c("packets_received_total", float64(p.Recv))
	c("packets_sent_total", float64(p.Sent))
	c("packets_total", float64(p.FloodRx), "rx", "flood")
	c("packets_total", float64(p.DirectRx), "rx", "direct")
	c("packets_total", float64(p.FloodTx), "tx", "flood")
	c("packets_total", float64(p.DirectTx), "tx", "direct")
	c("receive_errors_total", float64(p.RecvErrors))
	if e.hasNb {
		g("neighbors", float64(len(e.neighbors)))
		for _, n := range e.neighbors {
			g("neighbor_snr_db", n.SNR, n.ID)
			g("neighbor_last_heard_seconds", n.HeardAgo.Seconds(), n.ID)
		}
	}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// Serve listens on addr and serves /metrics (plus a tiny index at /) in the background.
func (e *Exporter) Serve(addr string) (net.Addr, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", e.Handler())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<html><head><title>meshtui exporter</title></head><body><h1>meshtui</h1><p><a href="/metrics">/metrics</a></p></body></html>`)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	return ln.Addr(), nil
}
