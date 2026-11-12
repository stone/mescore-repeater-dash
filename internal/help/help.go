// Package help holds the panel descriptions shared by the terminal and web UIs.
package help

// Section explains one dashboard panel.
type Section struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// Sections in display order. Key matches the panel ids used by the web UI.
var Sections = []Section{
	{"header", "Header", "LIVE / PAUSED / NO SIGNAL, repeater name, firmware, radio settings (frequency, spreading factor, bandwidth, coding rate), uptime since the last reboot, and battery."},
	{"radio", "Radio", "The antenna draws waves flying IN for packets received and OUT for packets transmitted or relayed. Below it: total rx/tx packets, and the RSSI/SNR of the last packet heard."},
	{"noise-rssi", "Noise · RSSI", "Yellow is the noise floor: background RF the radio hears when nobody talks. Cyan is the RSSI (signal strength) of the last received packet. The gap between them is your link margin."},
	{"snr", "SNR", "Signal-to-noise of each received packet, newest on the right. Green is clean. LoRa at SF8 still decodes down to about −10 dB, so red bars are packets just barely heard."},
	{"packets-min", "Packets / min", "One bar per poll. Received packets go UP from the center line, transmitted/relayed packets go DOWN."},
	{"stats", "Stats", "Duty is the % of time spent transmitting over the last 10 min (EU limit 10% on 869.4–869.65 MHz). Airtime is total tx/rx radio time. Flood packets are broadcast and relayed by everyone; direct packets follow a known route. Errors are packets that failed to decode. Queue holds packets waiting to send."},
	{"neighbors", "Neighbors", "Other repeaters heard directly, with their SNR and when they were last heard."},
	{"live-feed", "Live feed", "⇣ rx  ⇡ relayed  ⚠ decode errors  ⟳ reboot  ★ new neighbor  ϟ signal lost/restored  ⌘ your commands."},
	{"keys", "Keys", "h / ? help · : command prompt (tab completes, ↑↓ history) · t theme · p pause · q quit. Read-only commands run right away; anything that could change the repeater asks y/n first."},
}
