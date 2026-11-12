package main

import (
	"fmt"
	"net/http"
	"time"
)

// healthcheck probes url and fails on a network error or a 5xx response.
// A 401 counts as healthy: the server is up, it just wants credentials.
// It exists so distroless containers (no shell, no curl) can run a HEALTHCHECK.
func healthcheck(url string) error {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return nil
}
