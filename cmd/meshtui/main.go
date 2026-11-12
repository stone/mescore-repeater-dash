// Command meshtui is a live dashboard and Prometheus exporter for a MeshCore
// repeater on a serial port.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"meshtui/internal/exporter"
	"meshtui/internal/metrics"
	"meshtui/internal/repeater"
	"meshtui/internal/ui"
	"meshtui/internal/web"
)

const defaultListen = ":9110"

func main() {
	dev := flag.String("dev", "/dev/ttyUSB-heltecv3", "serial device of the repeater")
	baud := flag.Int("baud", 115200, "serial baud rate")
	interval := flag.Duration("interval", 2*time.Second, "stats poll interval")
	demo := flag.Bool("demo", false, "run on simulated data (no radio needed)")
	listen := flag.String("listen", "", "serve Prometheus /metrics on this address, e.g. :9110 (off when empty)")
	headless := flag.Bool("headless", false, "no TUI: only poll and serve -listen/-web (default -listen "+defaultListen+"); implied by -web")
	webAddr := flag.String("web", "", "serve the web dashboard on this address, e.g. :8080 (off when empty); runs without the TUI, access log on stdout, errors on stderr")
	webUser := flag.String("web-user", "", "basic auth user for the web dashboard")
	webPass := flag.String("web-pass", "", "basic auth password for the web dashboard (visible in ps; see -web-pass-file, $MESHTUI_WEB_PASS)")
	webPassFile := flag.String("web-pass-file", "", "read the web password from this file")
	webCert := flag.String("web-tls-cert", "", "serve the web dashboard over HTTPS with this certificate (PEM)")
	webKey := flag.String("web-tls-key", "", "private key (PEM) for -web-tls-cert")
	webConsole := flag.Bool("web-console", false, "allow running repeater commands from the web dashboard (needs -web-user/-web-pass)")
	health := flag.String("healthcheck", "", "probe this URL and exit 0 if it answers without a 5xx (for container healthchecks)")
	flag.Parse()

	if *health != "" {
		if err := healthcheck(*health); err != nil {
			fmt.Fprintln(os.Stderr, "meshtui: healthcheck:", err)
			os.Exit(1)
		}
		return
	}

	var src repeater.Source
	if *demo {
		src = repeater.NewDemo(time.Now().UnixNano())
	} else {
		s, err := repeater.OpenSerial(*dev, *baud)
		if err != nil {
			fmt.Fprintf(os.Stderr, "meshtui: open %s: %v\n", *dev, err)
			os.Exit(1)
		}
		src = s
	}
	defer src.Close()

	fail := func(f string, a ...any) { fmt.Fprintf(os.Stderr, "meshtui: "+f+"\n", a...); os.Exit(1) }
	if *webAddr != "" {
		*headless = true // the web dashboard replaces the TUI; the terminal shows logs
	}
	errLog := log.New(os.Stderr, "", log.LstdFlags)
	log.SetOutput(os.Stderr) // exporter and net/http errors
	if *headless && *listen == "" && *webAddr == "" {
		*listen = defaultListen
	}
	var obs []repeater.Observer
	var serving []string
	if *listen != "" {
		exp := exporter.New()
		addr, err := exp.Serve(*listen)
		if err != nil {
			fail("listen %s: %v", *listen, err)
		}
		obs = append(obs, exp)
		serving = append(serving, fmt.Sprintf("metrics on http://%s/metrics", addr))
	}
	if *webAddr != "" {
		pass, err := web.ResolvePassword(*webPass, *webPassFile, os.Getenv)
		if err != nil {
			fail("web password: %v", err)
		}
		ws, err := web.New(web.Config{Addr: *webAddr, User: *webUser, Pass: pass, Console: *webConsole,
			CertFile: *webCert, KeyFile: *webKey, AccessLog: os.Stdout, ErrorLog: errLog}, src)
		if err != nil {
			fail("web: %v", err)
		}
		addr, err := ws.Serve()
		if err != nil {
			fail("web %s: %v", *webAddr, err)
		}
		obs = append(obs, ws)
		scheme := "http"
		if *webCert != "" {
			scheme = "https"
		}
		serving = append(serving, fmt.Sprintf("web on %s://%s/ (auth %v, console %v)", scheme, addr, *webUser != "", *webConsole))
	} else if *webUser != "" || *webPass != "" || *webPassFile != "" || *webConsole || *webCert != "" {
		fail("-web-user/-web-pass/-web-console need -web ADDR")
	}
	if *headless {
		obs = append(obs, metrics.NewEventLog(errLog))
	}
	if len(obs) > 0 {
		src = repeater.Tee(src, obs...)
	}
	if *headless {
		what := *dev
		if *demo {
			what = "demo data"
		}
		errLog.Printf("meshtui: headless, polling %s every %s; %s", what, *interval, strings.Join(serving, "; "))
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		repeater.Run(ctx, src, *interval)
		errLog.Print("meshtui: stopped")
		return
	}

	info, err := src.Info()
	if err != nil {
		info = repeater.Info{Name: "?"}
	}
	if _, err := tea.NewProgram(ui.New(src, info, *interval)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "meshtui:", err)
		os.Exit(1)
	}
}
