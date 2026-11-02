package deploy

import (
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/store"
)

func TestSiteForCarriesTheWebOptions(t *testing.T) {
	a := store.App{Name: "loja", Domain: "loja.example.com", Port: 3000, Replicas: 1, Web: store.Web{
		Domains:     []store.WebDomain{{Name: "shop.example.com"}, {Name: "www.loja.example.com", To: "loja.example.com", Permanent: true}},
		Users:       []store.WebUser{{Name: "ana", Hash: "$2y$10$abc"}},
		Maintenance: true, MaxBodyMB: 50, ReadTimeout: 90,
	}}
	s := siteFor(a)
	if s.MaxBody != "50m" || s.ReadTimeout != 90 {
		t.Errorf("limites: %q %d", s.MaxBody, s.ReadTimeout)
	}
	if len(s.Extras) != 2 || s.Extras[1].To != "loja.example.com" || !s.Extras[1].Permanent {
		t.Errorf("extras: %+v", s.Extras)
	}
	if len(s.Users) != 1 || s.Users[0] != "ana:$2y$10$abc" {
		t.Errorf("usuários: %v", s.Users)
	}
	if s.Maintenance == nil || s.Maintenance.Title != defaultMaintTitle || !strings.Contains(s.Maintenance.Text, "Voltamos") {
		t.Errorf("manutenção sem texto usa o padrão: %+v", s.Maintenance)
	}
	a.Web = store.Web{Maintenance: true, MaintTitle: "Voltamos", MaintText: "Às 18h."}
	if m := siteFor(a).Maintenance; m == nil || m.Title != "Voltamos" || m.Text != "Às 18h." {
		t.Errorf("manutenção com texto: %+v", m)
	}
	if s := siteFor(store.App{Name: "x", Domain: "x.example.com", Port: 1}); s.MaxBody != "" || s.Extras != nil || s.Users != nil || s.Maintenance != nil {
		t.Errorf("app sem opções: %+v", s)
	}
}
