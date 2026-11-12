// Package repeater talks to a MeshCore repeater over its serial CLI.
package repeater

import "time"

// Core is the payload of `stats-core`.
type Core struct {
	BatteryMV  int `json:"battery_mv"`
	UptimeSecs int `json:"uptime_secs"`
	Errors     int `json:"errors"`
	QueueLen   int `json:"queue_len"`
}

// Radio is the payload of `stats-radio`.
type Radio struct {
	NoiseFloor int     `json:"noise_floor"`
	LastRSSI   int     `json:"last_rssi"`
	LastSNR    float64 `json:"last_snr"`
	TxAirSecs  int     `json:"tx_air_secs"`
	RxAirSecs  int     `json:"rx_air_secs"`
}

// Packets is the payload of `stats-packets`.
type Packets struct {
	Recv       int `json:"recv"`
	Sent       int `json:"sent"`
	FloodTx    int `json:"flood_tx"`
	DirectTx   int `json:"direct_tx"`
	FloodRx    int `json:"flood_rx"`
	DirectRx   int `json:"direct_rx"`
	RecvErrors int `json:"recv_errors"`
}

// Neighbor is one line of `neighbors`.
type Neighbor struct {
	ID       string
	HeardAgo time.Duration
	SNR      float64
}

// Info is static repeater identity and radio config.
type Info struct {
	Firmware string
	Name     string
	FreqMHz  float64
	BWkHz    float64
	SF, CR   int
}

// Sample is one poll result. Err is set when the stats could not be read.
type Sample struct {
	At           time.Time
	Core         Core
	Radio        Radio
	Packets      Packets
	Neighbors    []Neighbor
	HasNeighbors bool
	Err          error
}

// Source produces samples, from a real repeater or a simulation.
type Source interface {
	Info() (Info, error)
	Poll(withNeighbors bool) Sample
	// Exec sends one arbitrary CLI command (user-initiated, may change state).
	Exec(cmd string) (string, error)
	Close() error
}
