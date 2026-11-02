package store

import (
	"testing"
	"time"
)

func TestAutoScaleRoundTripAndSlots(t *testing.T) {
	s, _ := newTestStore(t)
	a := App{Name: "api", Repo: "https://github.com/x/y.git", Branch: "main", Port: 3100, Replicas: 2,
		AutoScale: AutoScale{Enabled: true, Min: 2, Max: 5, TargetCPU: 60, TargetMem: 70, OutCooldown: 30}}
	if err := s.Create(a); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("api")
	if got.AutoScale != a.AutoScale || !got.ScaledAt.IsZero() {
		t.Fatalf("%+v", got)
	}
	if got.Slots() != 5 {
		t.Errorf("com escalonamento, reserva o máximo: %d", got.Slots())
	}
	used := s.UsedPorts()
	for p := 3100; p <= 3104; p++ {
		if !used[p] {
			t.Errorf("porta %d deve estar reservada até o máximo", p)
		}
	}
	if used[3105] {
		t.Error("não reserva além do máximo")
	}

	// the autoscaler records its changes without touching the configuration
	at := time.Unix(1_800_000_000, 0)
	if err := s.SetScaled("api", 4, at); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get("api")
	if got.Replicas != 4 || !got.ScaledAt.Equal(at) || got.AutoScale != a.AutoScale {
		t.Fatalf("%+v", got)
	}
	// saving the form does not reset the cooldown clock
	got.RateLimit = 5
	if err := s.UpdateConfig(got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Get("api"); !again.ScaledAt.Equal(at) || again.RateLimit != 5 {
		t.Errorf("%+v", again)
	}

	// turning it off releases the reserved range
	got.AutoScale.Enabled = false
	_ = s.UpdateConfig(got)
	if s.UsedPorts()[3104] {
		t.Error("desligado, só as instâncias em execução ficam reservadas")
	}
	if again, _ := s.Get("api"); again.Slots() != 4 {
		t.Errorf("slots: %d", again.Slots())
	}
}

func TestAutoScaleDefaultCooldowns(t *testing.T) {
	var z AutoScale
	if z.OutCD() != 60*time.Second || z.InCD() != 300*time.Second {
		t.Errorf("%v %v", z.OutCD(), z.InCD())
	}
	if (AutoScale{OutCooldown: 10, InCooldown: 20}).OutCD() != 10*time.Second {
		t.Error("espera configurada")
	}
	if (App{Replicas: 0}).Slots() != 1 {
		t.Error("sem contagem há uma instância")
	}
}
