package main

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

var chartRanges = map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

type metricsResponse struct {
	Info   ServiceInfo `json:"info"`
	Points []Point     `json:"points"`
	Range  string      `json:"range"`
	From   int64       `json:"from"`
	To     int64       `json:"to"`
}

// handleMetrics returns the service status and the CPU/RAM history of one app.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	a, ok := s.appFor(w, r)
	if !ok {
		return
	}
	rg := r.URL.Query().Get("range")
	d, known := chartRanges[rg]
	if !known {
		rg, d = "1h", time.Hour
	}
	now := time.Now()
	pts := s.store.Metrics(a.Name, now.Add(-d), chartBucket(d))
	if pts == nil {
		pts = []Point{}
	}
	writeJSON(w, metricsResponse{Info: serviceInfo(a.Name), Points: pts, Range: rg, From: now.Add(-d).Unix(), To: now.Unix()})
}

type appOverview struct {
	State string   `json:"state"`
	CPU   *float64 `json:"cpu"`
	Mem   *int64   `json:"mem"`
}

// handleOverview feeds the dashboard: host totals and a live row per app.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"host": readHostStats(s.cfg.DataDir, runtime.NumCPU()), "apps": s.overview()})
}

func (s *Server) overview() map[string]appOverview {
	latest := s.store.LatestMetrics(time.Now().Add(-2 * sampleEvery))
	out := map[string]appOverview{}
	for _, a := range s.store.List() {
		o := appOverview{State: serviceState(a.Name)}
		if p, ok := latest[a.Name]; ok {
			cpu, mem := p.CPU, p.Mem
			o.CPU, o.Mem = &cpu, &mem
		}
		out[a.Name] = o
	}
	return out
}
