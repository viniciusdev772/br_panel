package autoscale

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/viniciusdev772/br_panel/internal/store"
)

var now = time.Unix(1_800_000_000, 0).Truncate(time.Hour) // a whole minute

// series returns n consecutive complete one-minute points ending one minute before now.
func series(n int, cpu float64, mem int64) []store.Point {
	pts := make([]store.Point, n)
	for i := range pts {
		pts[i] = store.Point{TS: now.Unix() - int64(n-i+1)*Bucket + Bucket, CPU: cpu, Mem: mem}
	}
	return pts
}

func app(cur, min, max, target int) store.App {
	return store.App{Name: "api", Replicas: cur, AutoScale: store.AutoScale{Enabled: true, Min: min, Max: max, TargetCPU: target}}
}

func TestScaleOutIsProportionalAndCapped(t *testing.T) {
	a := app(2, 1, 6, 60)
	// 2 instances, summed CPU 200% = 100% each, target 60%: 2*100/60 = 3.33 -> 4
	if d := Decide(a, series(3, 200, 0), now); d.Replicas != 4 || !strings.Contains(d.Reason, "acima da meta") {
		t.Fatalf("%+v", d)
	}
	// would be 2*400/60 = 13.3 -> capped at the maximum
	if d := Decide(a, series(3, 800, 0), now); d.Replicas != 6 {
		t.Errorf("deve respeitar o máximo: %+v", d)
	}
}

func TestScaleOutNeedsThreeConsecutiveMinutesAboveTarget(t *testing.T) {
	a := app(2, 1, 6, 60)
	if d := Decide(a, series(2, 200, 0), now); d.Replicas != 2 {
		t.Errorf("2 minutos não bastam: %+v", d)
	}
	pts := series(3, 200, 0)
	pts[1].CPU = 100 // one minute exactly at 50%: not above the target
	if d := Decide(a, pts, now); d.Replicas != 2 {
		t.Errorf("um minuto abaixo da meta interrompe: %+v", d)
	}
	pts = series(3, 200, 0)
	pts[0].TS -= Bucket // a missing minute in the window
	if d := Decide(a, pts, now); d.Replicas != 2 {
		t.Errorf("janela com buraco não vale: %+v", d)
	}
	stale := series(3, 200, 0)
	for i := range stale {
		stale[i].TS -= 10 * Bucket // app stopped being sampled
	}
	if d := Decide(a, stale, now); d.Replicas != 2 {
		t.Errorf("dados velhos não valem: %+v", d)
	}
}

func TestScaleInIsSlowAndNeedsMargin(t *testing.T) {
	a := app(4, 1, 6, 60)
	// 4 instances at 20% each (sum 80): ceil(4*20/60) = 2
	if d := Decide(a, series(InPeriods, 80, 0), now); d.Replicas != 2 || !strings.Contains(d.Reason, "abaixo da meta") {
		t.Fatalf("%+v", d)
	}
	if d := Decide(a, series(InPeriods-1, 80, 0), now); d.Replicas != 4 {
		t.Errorf("14 minutos não bastam: %+v", d)
	}
	// 57% is under the 60% target but not more than 10% under it
	if d := Decide(a, series(InPeriods, 4*57, 0), now); d.Replicas != 4 {
		t.Errorf("sem folga de 10%% não reduz: %+v", d)
	}
	// never below the minimum
	if d := Decide(app(4, 3, 6, 60), series(InPeriods, 8, 0), now); d.Replicas != 3 {
		t.Errorf("deve respeitar o mínimo: %+v", d)
	}
}

func TestCooldownsAndWarmup(t *testing.T) {
	a := app(2, 1, 6, 60)
	a.AutoScale.OutCooldown = 600
	a.ScaledAt = now.Add(-5 * time.Minute)
	if d := Decide(a, series(3, 200, 0), now); d.Replicas != 2 {
		t.Errorf("em espera depois de uma mudança: %+v", d)
	}
	a.AutoScale.OutCooldown = 0 // default 60 s
	if d := Decide(a, series(3, 200, 0), now); d.Replicas != 4 {
		t.Errorf("espera padrão já passou: %+v", d)
	}
	// metrics taken before the warmup of the last change describe the old capacity: ignored
	a.ScaledAt = now.Add(-90 * time.Second)
	if d := Decide(a, series(3, 200, 0), now); d.Replicas != 2 {
		t.Errorf("pontos do aquecimento não contam: %+v", d)
	}
	// a deploy restarts every instance, which counts as a change too
	b := app(2, 1, 6, 60)
	b.LastDeploy = now.Add(-90 * time.Second)
	if d := Decide(b, series(3, 200, 0), now); d.Replicas != 2 {
		t.Errorf("o pico de CPU de um deploy não deve escalar: %+v", d)
	}
	// scale-in has its own, longer, default cooldown (300 s)
	c := app(4, 1, 6, 60)
	c.ScaledAt = now.Add(-4 * time.Minute)
	if d := Decide(c, series(InPeriods, 80, 0), now.Add(0)); d.Replicas != 4 {
		t.Errorf("espera de descida: %+v", d)
	}
}

func TestOutsideTheRangeIsCorrected(t *testing.T) {
	if d := Decide(app(1, 3, 6, 60), nil, now); d.Replicas != 3 {
		t.Errorf("abaixo do mínimo: %+v", d)
	}
	if d := Decide(app(8, 1, 4, 60), nil, now); d.Replicas != 4 {
		t.Errorf("acima do máximo: %+v", d)
	}
}

func TestDisabledNeverScales(t *testing.T) {
	a := app(2, 1, 6, 60)
	a.AutoScale.Enabled = false
	if d := Decide(a, series(InPeriods, 800, 0), now); d.Replicas != 2 {
		t.Errorf("%+v", d)
	}
}

func TestCPUCapacityFollowsTheInstanceLimit(t *testing.T) {
	a := app(1, 1, 4, 50)
	a.CPUPercent = 200 // each instance may use 2 CPUs
	// 100% of one CPU is only 50% of the capacity: exactly at the target, not above it
	if d := Decide(a, series(3, 100, 0), now); d.Replicas != 1 {
		t.Errorf("%+v", d)
	}
	if d := Decide(a, series(3, 150, 0), now); d.Replicas != 2 { // 75% of capacity: ceil(1*75/50) = 2
		t.Errorf("%+v", d)
	}
}

func TestMemoryTargetAndMultiMetricRules(t *testing.T) {
	a := app(2, 1, 6, 60)
	a.MemoryMB, a.AutoScale.TargetMem = 512, 70
	mib := int64(1 << 20)
	// CPU idle, memory at 90% of the limit on each of 2 instances: scale out on memory alone (any metric)
	if d := Decide(a, series(3, 10, 2*460*mib), now); d.Replicas != 3 || !strings.Contains(d.Reason, "memória") {
		t.Errorf("%+v", d)
	}
	// scale in only when every metric allows it: CPU is low but memory is not
	if d := Decide(a, series(InPeriods, 10, 2*340*mib), now); d.Replicas != 2 {
		t.Errorf("memória alta impede a descida: %+v", d)
	}
	// both low: scale in to the capacity the hungrier metric needs (memory: 2*20/70 -> 1)
	if d := Decide(a, series(InPeriods, 10, 2*100*mib), now); d.Replicas != 1 {
		t.Errorf("%+v", d)
	}
	// a memory target without a memory limit is ignored
	a.MemoryMB = 0
	if d := Decide(a, series(3, 10, 9999*mib), now); d.Replicas != 2 {
		t.Errorf("%+v", d)
	}
}

type fakeScaler struct {
	calls []int
	err   error
}

func (f *fakeScaler) Scale(_ context.Context, _ store.App, n int, _ io.Writer) error {
	f.calls = append(f.calls, n)
	return f.err
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestTickScalesDeployedAppsOnly(t *testing.T) {
	st := openStore(t)
	for _, name := range []string{"api", "novo"} {
		a := app(2, 1, 6, 60)
		a.Name, a.Repo, a.Branch, a.StartCmd = name, "https://github.com/o/r.git", "main", "x"
		a.Port = map[string]int{"api": 3100, "novo": 3200}[name]
		if err := st.Create(a); err != nil {
			t.Fatal(err)
		}
		for _, p := range series(3, 200, 0) { // both apps are overloaded
			_ = st.InsertMetrics(name, []store.Point{p})
		}
	}
	st.BeginDeploy("api")
	st.FinishDeploy("api", true) // "novo" never deployed

	fs := &fakeScaler{}
	tick(context.Background(), st, fs, now)
	if len(fs.calls) != 1 || fs.calls[0] != 4 {
		t.Fatalf("só o app publicado escala: %v", fs.calls)
	}
	if ev := st.Events(5); len(ev) == 0 || !strings.Contains(ev[0].Message, "2 → 4 instâncias") || ev[0].Resource != "api" {
		t.Errorf("eventos: %+v", ev)
	}
}

func TestFailedScaleStartsTheCooldownAndLeavesAnEvent(t *testing.T) {
	st := openStore(t)
	a := app(2, 1, 6, 60)
	a.Repo, a.Branch, a.StartCmd, a.Port = "https://github.com/o/r.git", "main", "x", 3100
	if err := st.Create(a); err != nil {
		t.Fatal(err)
	}
	fs := &fakeScaler{err: errors.New("sem portas")}
	scale(context.Background(), st, fs, a, 2, Decision{4, "teste"}, now)
	got, _ := st.Get("api")
	if got.ScaledAt.Unix() != now.Unix() || got.Replicas != 2 {
		t.Errorf("tentativa falha deve só iniciar a espera: %+v", got)
	}
	if ev := st.Events(5); len(ev) == 0 || !strings.Contains(ev[0].Message, "falhou") {
		t.Errorf("eventos: %+v", ev)
	}
}
