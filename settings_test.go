package main

import (
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	h, err := hashPassword("uma senha bem longa")
	if err != nil || !strings.HasPrefix(h, "$argon2id$v=19$m=32768,t=3,p=1$") {
		t.Fatalf("%q %v", h, err)
	}
	if !verifyPassword("uma senha bem longa", h) || verifyPassword("outra", h) {
		t.Error("verify")
	}
	h2, _ := hashPassword("uma senha bem longa")
	if h == h2 {
		t.Error("salt deve ser aleatório")
	}
	for _, bad := range []string{"", "x", "$argon2id$v=19$m=999999999,t=3,p=1$AA$AA", "$bcrypt$a$b$c$d"} {
		if verifyPassword("x", bad) {
			t.Errorf("hash inválido aceito: %q", bad)
		}
	}
	if validatePassword("curta") == nil || validatePassword(strings.Repeat("a", 129)) == nil || validatePassword(strings.Repeat("a", 12)) != nil {
		t.Error("validatePassword")
	}
}

func TestValidateSettings(t *testing.T) {
	ok := defaultSettings()
	if err := validateSettings(ok); err != nil {
		t.Fatal(err)
	}
	mut := func(f func(*Settings)) Settings { s := defaultSettings(); f(&s); return s }
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
		if validateSettings(s) == nil {
			t.Errorf("%s deveria falhar", name)
		}
	}
	good := mut(func(s *Settings) {
		s.PanelDomain, s.AcmeEmail, s.AppUser = "painel.exemplo.com", "admin@exemplo.com", "brapps"
		s.GitHubWebhookSecret = strings.Repeat("s", 20)
	})
	if err := validateSettings(good); err != nil {
		t.Error(err)
	}
}
