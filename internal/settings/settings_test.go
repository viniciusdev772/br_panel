package settings

import (
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("uma senha bem longa")
	if err != nil || !strings.HasPrefix(h, "$argon2id$v=19$m=32768,t=3,p=1$") {
		t.Fatalf("%q %v", h, err)
	}
	if !VerifyPassword("uma senha bem longa", h) || VerifyPassword("outra", h) {
		t.Error("verify")
	}
	h2, _ := HashPassword("uma senha bem longa")
	if h == h2 {
		t.Error("salt deve ser aleatório")
	}
	for _, bad := range []string{"", "x", "$argon2id$v=19$m=999999999,t=3,p=1$AA$AA", "$bcrypt$a$b$c$d"} {
		if VerifyPassword("x", bad) {
			t.Errorf("hash inválido aceito: %q", bad)
		}
	}
	if ValidatePassword("curta") == nil || ValidatePassword(strings.Repeat("a", 129)) == nil || ValidatePassword(strings.Repeat("a", 12)) != nil {
		t.Error("ValidatePassword")
	}
}

func TestValidateSettings(t *testing.T) {
	ok := Default()
	if err := Validate(ok); err != nil {
		t.Fatal(err)
	}
	mut := func(f func(*Settings)) Settings { s := Default(); f(&s); return s }
	bad := map[string]Settings{
		"domínio":        mut(func(s *Settings) { s.PanelDomain = "localhost" }),
		"email":          mut(func(s *Settings) { s.AcmeEmail = "x" }),
		"email com nome": mut(func(s *Settings) { s.AcmeEmail = "A <a@b.com>" }),
		"usuário":        mut(func(s *Settings) { s.AppUser = "Root;" }),
		"dir relativo":   mut(func(s *Settings) { s.NginxDir = "etc/nginx" }),
		"dir injeção":    mut(func(s *Settings) { s.AcmeRoot = "/var/www;x" }),
		"app sem chave":  mut(func(s *Settings) { s.GitHubAppID = "123" }),
		"chave inválida": mut(func(s *Settings) { s.GitHubAppID, s.GitHubKey = "123", "não é pem" }),
		"segredo curto":  mut(func(s *Settings) { s.GitHubWebhookSecret = "curto" }),
	}
	for name, s := range bad {
		if Validate(s) == nil {
			t.Errorf("%s deveria falhar", name)
		}
	}
	good := mut(func(s *Settings) {
		s.PanelDomain, s.AcmeEmail, s.AppUser = "painel.exemplo.com", "admin@exemplo.com", "brapps"
		s.GitHubWebhookSecret = strings.Repeat("s", 20)
	})
	if err := Validate(good); err != nil {
		t.Error(err)
	}
}
