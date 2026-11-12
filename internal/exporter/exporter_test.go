package exporter

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"meshtui/internal/repeater"
)

func scrape(t *testing.T, e *Exporter) string {
	t.Helper()
	rec := httptest.NewRecorder()
	e.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	b, _ := io.ReadAll(rec.Body)
	return string(b)
}

func good() repeater.Sample {
	return repeater.Sample{
		At:      time.Unix(1_800_000_000, 0),
		Core:    repeater.Core{BatteryMV: 4331, UptimeSecs: 18637, Errors: 0, QueueLen: 2},
		Radio:   repeater.Radio{NoiseFloor: -111, LastRSSI: -106, LastSNR: 4.25, TxAirSecs: 225, RxAirSecs: 235},
		Packets: repeater.Packets{Recv: 350, Sent: 325, FloodTx: 292, DirectTx: 33, FloodRx: 298, DirectRx: 52, RecvErrors: 14},
	}
}

func mustHave(t *testing.T, body string, lines ...string) {
	t.Helper()
	for _, l := range lines {
		if !strings.Contains(body, l+"\n") {
			t.Errorf("missing %q", l)
		}
	}
}

func TestBeforeFirstPoll(t *testing.T) {
	body := scrape(t, New())
	mustHave(t, body, "meshcore_up 0")
	if strings.Contains(body, "meshcore_battery_volts") {
		t.Fatal("no device metrics before first sample")
	}
}

func TestAfterGoodPoll(t *testing.T) {
	e := New()
	e.SetInfo(repeater.Info{Firmware: "v1.18.0-81dbae5", Name: "SE-KSD-SkareRep", FreqMHz: 869.618, BWkHz: 62.5, SF: 8, CR: 8})
	e.Observe(good())
	mustHave(t, scrape(t, e),
		"meshcore_up 1",
		`meshcore_info{bw_khz="62.5",cr="8",firmware="v1.18.0-81dbae5",freq_mhz="869.618",name="SE-KSD-SkareRep",sf="8"} 1`,
		"meshcore_battery_volts 4.331",
		"meshcore_uptime_seconds 18637",
		"meshcore_queue_length 2",
		"meshcore_noise_floor_dbm -111",
		"meshcore_last_rssi_dbm -106",
		"meshcore_last_snr_db 4.25",
		"meshcore_tx_air_seconds_total 225",
		"meshcore_rx_air_seconds_total 235",
		"meshcore_packets_received_total 350",
		"meshcore_packets_sent_total 325",
		`meshcore_packets_total{direction="rx",route="flood"} 298`,
		`meshcore_packets_total{direction="tx",route="direct"} 33`,
		"meshcore_receive_errors_total 14",
		"meshcore_last_success_timestamp_seconds 1.8e+09",
	)
}

func TestNeighborsKeptBetweenPolls(t *testing.T) {
	e := New()
	s := good()
	s.HasNeighbors, s.Neighbors = true, []repeater.Neighbor{{ID: "A1B2C3D4", HeardAgo: 120 * time.Second, SNR: 5}}
	e.Observe(s)
	e.Observe(good()) // no neighbor query this poll
	mustHave(t, scrape(t, e),
		"meshcore_neighbors 1",
		`meshcore_neighbor_snr_db{id="A1B2C3D4"} 5`,
		`meshcore_neighbor_last_heard_seconds{id="A1B2C3D4"} 120`,
	)
}

func TestNoSignalHidesDeviceMetrics(t *testing.T) {
	e := New()
	e.Observe(good())
	for i := 0; i < 3; i++ {
		e.Observe(repeater.Sample{Err: errors.New("timeout")})
	}
	body := scrape(t, e)
	mustHave(t, body, "meshcore_up 0", "meshcore_poll_failures_total 3")
	if strings.Contains(body, "meshcore_battery_volts") {
		t.Fatal("stale device metrics exported during NO SIGNAL")
	}
	e.Observe(good())
	mustHave(t, scrape(t, e), "meshcore_up 1", "meshcore_battery_volts 4.331")
}

func TestOneFailureKeepsLastValues(t *testing.T) {
	e := New()
	e.Observe(good())
	e.Observe(repeater.Sample{Err: errors.New("timeout")})
	mustHave(t, scrape(t, e), "meshcore_up 1", "meshcore_battery_volts 4.331", "meshcore_poll_failures_total 1")
}

func TestExporterIsObserver(t *testing.T) {
	e := New()
	src := repeater.Tee(repeater.NewDemo(1), e)
	src.Info()
	src.Poll(true)
	body := scrape(t, e)
	mustHave(t, body, "meshcore_up 1", `meshcore_info{bw_khz="62.5",cr="8",firmware="v1.18.0-demo",freq_mhz="869.618",name="SE-DEMO-Repeater",sf="8"} 1`)
	if !strings.Contains(body, "meshcore_neighbors ") {
		t.Fatal("neighbors missing")
	}
}

func TestServeHTTP(t *testing.T) {
	e := New()
	addr, err := e.Serve("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"/metrics": "meshcore_up 0", "/": `href="/metrics"`} {
		resp, err := http.Get("http://" + addr.String() + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(b), want) {
			t.Fatalf("%s: %d %q", path, resp.StatusCode, b)
		}
	}
	if _, err := e.Serve(addr.String()); err == nil {
		t.Fatal("second Serve on the same port should fail")
	}
}
