package main

import (
	"encoding/json"
	"net/http"
	"time"
)


// HTTP handler for /discover
func handleDiscover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	timeout := 6 * time.Second
	if t := r.URL.Query().Get("timeout"); t != "" {
		if d, err := time.ParseDuration(t); err == nil {
			timeout = d
		}
	}

	// Use new scanner's DiscoverNow (quick discovery)
	out, err := DiscoverNow(ctx, timeout)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b, _ := json.Marshal(out)
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}
