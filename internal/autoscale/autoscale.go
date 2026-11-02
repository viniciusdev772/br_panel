// Package autoscale keeps the number of instances of an app in step with its load, the way AWS
// target tracking scaling policies do for ECS services (Application Auto Scaling):
//
//   - you give a minimum and a maximum number of instances and a target for the average
//     utilization (CPU, optionally memory) of the running instances;
//   - it scales out quickly and proportionally when the metric stays above the target, and scales
//     in slowly, only after a long stretch well below it;
//   - after a change, new instances get a warmup before they count, and cooldowns block repeated
//     changes;
//   - with several metrics it scales out when any asks for it and scales in only when all agree.
package autoscale

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/viniciusdev772/br_panel/internal/store"
)

const (
	// Tick is how often every app is evaluated.
	Tick = 30 * time.Second
	// Bucket is the metric granularity in seconds. AWS evaluates one-minute metrics.
	Bucket = 60
	// OutPeriods is how many consecutive minutes the metric must stay above the target to scale out
	// (AWS target tracking uses 3 data points).
	OutPeriods = 3
	// InPeriods is how many consecutive minutes it must stay below the target to scale in
	// (AWS uses 15 data points: scale-in is deliberately slow).
	InPeriods = 15
	// InMargin keeps scale-in from flapping: the average must be under this fraction of the target,
	// which AWS describes as usually more than 10% lower.
	InMargin = 0.9
	// Warmup is how long after a change (or a deploy) the metrics are ignored, because starting
	// instances use more CPU than they will in steady state.
	Warmup = 60 * time.Second

	scaleTimeout = 5 * time.Minute
)

// Scaler changes the number of running instances of an app; deploy.Deployer implements it.
type Scaler interface {
	Scale(ctx context.Context, a store.App, n int, log io.Writer) error
}

// Decision is the wanted instance count and why (empty reason when nothing changes).
type Decision struct {
	Replicas int
	Reason   string
}

type metric struct {
	name   string
	target float64
	util   func(store.Point) float64 // utilization in %, per instance
}

func metricsOf(a store.App, cur int) []metric {
	capCPU := 100.0 // one CPU, unless the instance has its own CPU limit
	if a.CPUPercent > 0 {
		capCPU = float64(a.CPUPercent)
	}
	ms := []metric{{"CPU", float64(a.AutoScale.TargetCPU), func(p store.Point) float64 { return p.CPU / float64(cur) / capCPU * 100 }}}
	if a.AutoScale.TargetMem > 0 && a.MemoryMB > 0 {
		capMem := float64(a.MemoryMB) * (1 << 20)
		ms = append(ms, metric{"memória", float64(a.AutoScale.TargetMem), func(p store.Point) float64 { return float64(p.Mem) / float64(cur) / capMem * 100 }})
	}
	return ms
}

// Decide applies the target tracking rules to the one-minute metric points of an app (oldest
// first, as store.Metrics returns them).
func Decide(a store.App, pts []store.Point, now time.Time) Decision {
	cfg, cur := a.AutoScale, max(a.Replicas, 1)
	keep := Decision{Replicas: cur}
	if !cfg.Enabled {
		return keep
	}
	sinceChange := now.Sub(a.ScaledAt)
	// Outside the configured range: bring it back, no metrics needed (AWS does the same).
	if cur < cfg.Min || cur > cfg.Max {
		if sinceChange < cfg.OutCD() {
			return keep
		}
		to := min(max(cur, cfg.Min), cfg.Max)
		return Decision{to, fmt.Sprintf("fora da faixa de %d a %d instâncias", cfg.Min, cfg.Max)}
	}

	// Only complete minutes that started after the warmup of the last change or deploy.
	ref := a.ScaledAt
	if a.LastDeploy.After(ref) {
		ref = a.LastDeploy
	}
	var valid []store.Point
	for _, p := range pts {
		if p.TS >= ref.Add(Warmup).Unix() && p.TS+Bucket <= now.Unix() {
			valid = append(valid, p)
		}
	}
	// The window must be unbroken and recent: a missing minute means the app was down or not
	// sampled, and AWS does not scale on missing data either.
	window := func(n int) []store.Point {
		if len(valid) < n {
			return nil
		}
		w := valid[len(valid)-n:]
		if w[n-1].TS-w[0].TS != int64(n-1)*Bucket || w[n-1].TS+2*Bucket < now.Unix() {
			return nil
		}
		return w
	}
	ms := metricsOf(a, cur)

	if w := window(OutPeriods); w != nil {
		want, why := cur, ""
		for _, m := range ms {
			avg, all := average(w, m, func(u float64) bool { return u > m.target })
			if all {
				if n := int(math.Ceil(float64(cur) * avg / m.target)); n > want {
					want = n
					why = fmt.Sprintf("%s média de %.0f%% acima da meta de %.0f%% nos últimos %d minutos", m.name, avg, m.target, OutPeriods)
				}
			}
		}
		if want = min(want, cfg.Max); want > cur && sinceChange >= cfg.OutCD() {
			return Decision{want, why}
		}
	}

	if w := window(InPeriods); w != nil {
		want, why, ok := 0, "", true
		for _, m := range ms {
			avg, all := average(w, m, func(u float64) bool { return u < m.target })
			if !all || avg >= m.target*InMargin {
				ok = false
				break
			}
			if n := int(math.Ceil(float64(cur) * avg / m.target)); n > want { // the metric that needs the most capacity decides
				want = n
				why = fmt.Sprintf("%s média de %.0f%% abaixo da meta de %.0f%% nos últimos %d minutos", m.name, avg, m.target, InPeriods)
			}
		}
		if want = max(want, cfg.Min); ok && want < cur && sinceChange >= cfg.InCD() {
			return Decision{want, why}
		}
	}
	return keep
}

// average returns the mean utilization over pts and whether every point satisfies cond.
func average(pts []store.Point, m metric, cond func(float64) bool) (avg float64, all bool) {
	all = true
	for _, p := range pts {
		u := m.util(p)
		avg += u
		all = all && cond(u)
	}
	return avg / float64(len(pts)), all
}

// Run evaluates every autoscaled app each Tick until ctx ends.
func Run(ctx context.Context, st *store.Store, sc Scaler) {
	t := time.NewTicker(Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			tick(ctx, st, sc, now)
		}
	}
}

func tick(ctx context.Context, st *store.Store, sc Scaler, now time.Time) {
	for _, a := range st.List() {
		// A deploy rewrites the instances itself; and an app that never deployed has nothing to scale.
		if !a.AutoScale.Enabled || a.Status != store.StatusDeployed {
			continue
		}
		d := Decide(a, st.Metrics(a.Name, now.Add(-(InPeriods+3)*time.Minute), Bucket), now)
		if cur := max(a.Replicas, 1); d.Replicas != cur {
			scale(ctx, st, sc, a, cur, d, now)
		}
	}
}

func scale(ctx context.Context, st *store.Store, sc Scaler, a store.App, cur int, d Decision, now time.Time) {
	ctx, cancel := context.WithTimeout(ctx, scaleTimeout)
	defer cancel()
	if err := sc.Scale(ctx, a, d.Replicas, io.Discard); err != nil {
		// Record the attempt so the cooldown also spaces out retries of something that keeps failing.
		_ = st.SetScaled(a.Name, cur, now)
		st.AddEvent("app", a.Name, fmt.Sprintf("Escalonamento automático falhou (%d → %d instâncias): %v", cur, d.Replicas, err))
		return
	}
	st.AddEvent("app", a.Name, fmt.Sprintf("Escalonamento automático: %d → %d instâncias (%s)", cur, d.Replicas, d.Reason))
}
