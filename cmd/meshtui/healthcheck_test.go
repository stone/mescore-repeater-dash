package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	for _, tc := range []struct {
		status int
		ok     bool
	}{
		{http.StatusOK, true},
		{http.StatusUnauthorized, true}, // web dashboard behind basic auth is still up
		{http.StatusInternalServerError, false},
		{http.StatusServiceUnavailable, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
		}))
		err := healthcheck(srv.URL)
		srv.Close()
		if (err == nil) != tc.ok {
			t.Errorf("status %d: err=%v, want ok=%v", tc.status, err, tc.ok)
		}
	}
	if err := healthcheck("http://127.0.0.1:1/"); err == nil {
		t.Error("closed port: want error")
	}
}
