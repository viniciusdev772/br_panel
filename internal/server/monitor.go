package server

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/viniciusdev772/br_panel/internal/monitor"
	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/sysinfo"
	"github.com/viniciusdev772/br_panel/internal/systemd"
)

var chartRanges = map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

type metricsResponse struct {
	Info   systemd.ServiceInfo `json:"info"`
	Points []store.Point       `json:"points"`
	Range  string              `json:"range"`
	From   int64               `json:"from"`
	To     int64               `json:"to"`
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
	pts := s.store.Metrics(a.Name, now.Add(-d), monitor.Bucket(d))
	if pts == nil {
		pts = []store.Point{}
	}
	writeJSON(w, metricsResponse{Info: systemd.Info(a.Name), Points: pts, Range: rg, From: now.Add(-d).Unix(), To: now.Unix()})
}

// resOverview is the live state of one resource (app or database) on the canvas.
type resOverview struct {
	Type   string   `json:"type"`   // app | db
	State  string   `json:"state"`  // systemd ActiveState
	Status string   `json:"status"` // deploy status (apps) or provisioning status (databases)
	CPU    *float64 `json:"cpu"`
	Mem    *int64   `json:"mem"`
}

// handleOverview feeds the canvas: host totals, a live row per resource and the activity feed.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"host":   sysinfo.ReadHost(s.cfg.DataDir, runtime.NumCPU()),
		"res":    s.overview(),
		"events": s.store.Events(30),
	})
}

func (s *Server) overview() map[string]resOverview {
	latest := s.store.LatestMetrics(time.Now().Add(-2 * monitor.SampleEvery))
	out := map[string]resOverview{}
	put := func(name, typ, unit, status, metricsKey string) {
		o := resOverview{Type: typ, State: systemd.StateUnit(unit), Status: status}
		if p, ok := latest[metricsKey]; ok {
			cpu, mem := p.CPU, p.Mem
			o.CPU, o.Mem = &cpu, &mem
		}
		out[name] = o
	}
	for _, a := range s.store.List() {
		put(a.Name, "app", systemd.UnitName(a.Name), a.Status, a.Name)
	}
	for _, d := range s.store.ListDatabases() {
		put(d.Name, "db", systemd.DBUnitName(d.Name), d.Status, monitor.DBKey(d.Name))
	}
	return out
}
