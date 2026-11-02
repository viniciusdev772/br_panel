package store

import (
	"path/filepath"
	"testing"
)

func TestWebOptionsAreSavedApartFromTheAppForm(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Create(App{Name: "loja", Repo: "https://example.com/a.git", Branch: "main", Port: 3000, Domain: "loja.example.com"}); err != nil {
		t.Fatal(err)
	}
	w := Web{
		Domains:     []WebDomain{{Name: "www.loja.example.com", To: "loja.example.com", Permanent: true}, {Name: "shop.example.com"}},
		Users:       []WebUser{{Name: "ana", Hash: "$2y$10$x"}},
		Maintenance: true, MaintTitle: "Já voltamos", MaintText: "Atualizando.", MaxBodyMB: 50, ReadTimeout: 120,
	}
	if err := st.SetWeb("loja", w); err != nil {
		t.Fatal(err)
	}
	a, _ := st.Get("loja")
	if len(a.Web.Domains) != 2 || a.Web.Users[0].Hash != "$2y$10$x" || !a.Web.Maintenance || a.Web.MaxBodyMB != 50 || a.Web.ReadTimeout != 120 {
		t.Fatalf("web = %+v", a.Web)
	}
	// Saving the app form must not erase them.
	a.Branch = "dev"
	if err := st.UpdateConfig(a); err != nil {
		t.Fatal(err)
	}
	if b, _ := st.Get("loja"); len(b.Web.Domains) != 2 || b.Branch != "dev" {
		t.Fatalf("UpdateConfig apagou as opções do site: %+v", b.Web)
	}
	// Extra domains count as taken.
	if n, ok := st.DomainOwner("shop.example.com"); !ok || n != "loja" {
		t.Errorf("DomainOwner(extra) = %q, %v", n, ok)
	}
	if _, ok := st.DomainOwner("livre.example.com"); ok {
		t.Error("domínio livre apareceu como usado")
	}
	if err := st.SetWeb("loja", Web{}); err != nil {
		t.Fatal(err)
	}
	if b, _ := st.Get("loja"); len(b.Web.Domains) != 0 || b.Web.Maintenance {
		t.Errorf("limpar deveria zerar: %+v", b.Web)
	}
}
