package main

import "testing"

func TestValidateRepo(t *testing.T) {
	ok := []string{
		"https://github.com/user/repo.git",
		"git@github.com:user/repo.git",
		"ssh://git@example.com/user/repo",
	}
	bad := []string{
		"", "-oProxyCommand=x", "ext::sh -c id", "file:///etc", "http://x.com/a",
		"https://user:tok@github.com/a/b", "https://github.com", "https://a.com/a b",
		"--upload-pack=x", "ssh://root@example.com/x",
	}
	for _, s := range ok {
		if err := validateRepo(s); err != nil {
			t.Errorf("%q deveria ser válido: %v", s, err)
		}
	}
	for _, s := range bad {
		if validateRepo(s) == nil {
			t.Errorf("%q deveria ser inválido", s)
		}
	}
}

func TestValidateNameBranch(t *testing.T) {
	for _, s := range []string{"app", "my-app-2"} {
		if validateName(s) != nil {
			t.Errorf("name %q", s)
		}
	}
	for _, s := range []string{"", "A", "a", "1app", "../x", "a/b", "app_x"} {
		if validateName(s) == nil {
			t.Errorf("name %q deveria falhar", s)
		}
	}
	for _, s := range []string{"main", "feature/x-1", "v1.2.3"} {
		if validateBranch(s) != nil {
			t.Errorf("branch %q", s)
		}
	}
	for _, s := range []string{"", "-x", "a..b", "x.lock", "a b", "a/"} {
		if validateBranch(s) == nil {
			t.Errorf("branch %q deveria falhar", s)
		}
	}
}

func TestParseEnv(t *testing.T) {
	kv, err := parseEnv("# c\nA=1\n\nB = x=y\r\n")
	if err != nil || len(kv) != 2 || kv[1][1] != " x=y" && kv[1][1] != "x=y" {
		t.Fatalf("%v %v", kv, err)
	}
	if _, err := parseEnv("1BAD=x"); err == nil {
		t.Error("esperava erro")
	}
	if _, err := parseEnv("semigual"); err == nil {
		t.Error("esperava erro")
	}
}
