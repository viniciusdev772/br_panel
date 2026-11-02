package deploy

import (
	"testing"

	"github.com/viniciusdev772/br_panel/internal/store"
)

func TestAutoscaleReservesPortsUpToTheMaximum(t *testing.T) {
	s := openTestStore(t)
	d := &Deployer{Store: s}
	// 1 instance running now, but it may grow to 4: ports 3100..3103 are its own
	_ = s.Create(store.App{Name: "api", Repo: "r", Branch: "main", StartCmd: "x", Port: 3100, Replicas: 1,
		AutoScale: store.AutoScale{Enabled: true, Min: 1, Max: 4, TargetCPU: 60}})
	if msg := d.PortConflict(store.App{Name: "web", Port: 3102, Replicas: 1}); msg == "" {
		t.Error("a faixa até o máximo de api não pode ser usada por outra app")
	}
	if msg := d.PortConflict(store.App{Name: "web", Port: 3104, Replicas: 1}); msg != "" {
		t.Errorf("depois do máximo está livre: %s", msg)
	}
	if p := d.PickPortRange("", 1); p < 3104 && p != 0 && p >= 3100 {
		t.Errorf("app nova não pode cair dentro da faixa reservada: %d", p)
	}
	// growing the maximum over another app's ports is refused
	_ = s.Create(store.App{Name: "web", Repo: "r", Branch: "main", StartCmd: "x", Port: 3104, Replicas: 1})
	grown := store.App{Name: "api", Port: 3100, Replicas: 1, AutoScale: store.AutoScale{Enabled: true, Min: 1, Max: 6, TargetCPU: 60}}
	if msg := d.PortConflict(grown); msg == "" {
		t.Error("máximo maior que colide com web deve ser recusado")
	}
}
