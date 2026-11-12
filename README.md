# meshtui

Live dashboard for a MeshCore repeater on a serial console. It only sends read-only CLI commands.

    make deploy                       # build static binary, copy to neipa.wg
    ssh -t neipa.wg ./meshtui         # run against /dev/ttyUSB-heltecv3
    ./meshtui -demo                   # simulated data, no radio needed
    keys: h help · : command prompt · t theme · p pause · q quit
    flags: -dev -baud -interval -demo

The : prompt sends any CLI command. Read-only ones (get …, ver, clock, neighbors, log, stats-*)
run at once; anything else asks y/n first. Automatic polling is always read-only.

## Prometheus

    ./meshtui -listen :9110           # dashboard + /metrics
    ./meshtui -headless               # no TUI, only /metrics on :9110 (systemd: deploy/meshtui.service)

Scrape config: `- job_name: meshcore` / `static_configs: [{targets: ['neipa.wg:9110']}]`.
Metrics: meshcore_up, meshcore_info, battery/uptime/queue, noise floor, RSSI/SNR, airtime and
packet counters (by rx/tx, flood/direct), receive errors and per-neighbor SNR. Device metrics are
withheld while the radio is silent (3+ failed polls), so alert on `meshcore_up == 0`.

## Web dashboard

    ./meshtui -web :8080 -web-user admin -web-pass 'secret'               # browser dashboard, no TUI
    ./meshtui -web :8080 -web-user admin -web-pass 'secret' -web-console  # + command console
    ./meshtui -web :8080 -listen :9110 >access.log 2>error.log           # + /metrics, logs to files

With `-web` there is no TUI. The terminal shows logs instead:

- stdout: access log, one Combined Log Format line per request plus duration. SSE streams are
  logged when they close. A rejected login is logged as user `-`.
- stderr: startup, repeater identity, signal lost/restored, reboots, HTTP server errors and an
  audit line for every console command (`exec user=admin from IP: "cmd" → "reply"`, secrets redacted).

Everything is embedded in the binary: plain HTML/CSS/JS (~42 KB), no CDN, no build step. Live
updates are pushed over Server-Sent Events. `-web-console` adds a command console. It needs basic
auth, and anything not read-only asks for confirmation. Command strings and passwords are redacted
in the shared live feed.

- Password: `-web-pass` (visible in `ps`), `-web-pass-file FILE`, or `MESHTUI_WEB_PASS=…`.
- HTTPS: `-web-tls-cert cert.pem -web-tls-key key.pem`, or put it behind a reverse proxy. Over
  WireGuard the link is already encrypted.
- At most 32 browser connections at once; a stalled browser is dropped after 10 s.

## Docker

    docker build -t meshtui:local .
    docker run --rm -it meshtui:local -demo                    # TUI on demo data
    docker compose up -d                                       # web :8080 + /metrics :9110 (see docker-compose.yaml header)
    docker compose stop web && docker compose run --rm tui     # TUI on the real radio

The image is distroless, runs as UID 65532, and has a read-only root filesystem. By default it runs
the web dashboard. Passing your own arguments replaces the default command: without `-web` or
`-headless` it starts the TUI. Serial access needs `--device` plus `--group-add <dialout GID>`.

Release: push a `v*` tag that points at a commit on `main`. `.github/workflows/release-image.yaml`
tests, builds linux/amd64 and linux/arm64 images, and pushes them to `ghcr.io/<owner>/<repo>`.
You can push to a different registry by setting the `REGISTRY`, `IMAGE_NAME` and
`REGISTRY_USERNAME` variables and the `REGISTRY_PASSWORD` secret.
