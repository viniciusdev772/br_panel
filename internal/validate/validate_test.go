package validate

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
		if err := Repo(s); err != nil {
			t.Errorf("%q deveria ser válido: %v", s, err)
		}
	}
	for _, s := range bad {
		if Repo(s) == nil {
			t.Errorf("%q deveria ser inválido", s)
		}
	}
}

func TestValidateNameBranch(t *testing.T) {
	for _, s := range []string{"app", "my-app-2"} {
		if Name(s) != nil {
			t.Errorf("name %q", s)
		}
	}
	for _, s := range []string{"", "A", "a", "1app", "../x", "a/b", "app_x"} {
		if Name(s) == nil {
			t.Errorf("name %q deveria falhar", s)
		}
	}
	for _, s := range []string{"main", "feature/x-1", "v1.2.3"} {
		if Branch(s) != nil {
			t.Errorf("branch %q", s)
		}
	}
	for _, s := range []string{"", "-x", "a..b", "x.lock", "a b", "a/"} {
		if Branch(s) == nil {
			t.Errorf("branch %q deveria falhar", s)
		}
	}
}

func TestParseEnv(t *testing.T) {
	kv, err := ParseEnv("# c\nA=1\n\nB = x=y\r\n")
	if err != nil || len(kv) != 2 || kv[1][1] != " x=y" && kv[1][1] != "x=y" {
		t.Fatalf("%v %v", kv, err)
	}
	if _, err := ParseEnv("1BAD=x"); err == nil {
		t.Error("esperava erro")
	}
	if _, err := ParseEnv("semigual"); err == nil {
		t.Error("esperava erro")
	}
}

func TestValidateDomain(t *testing.T) {
	for _, d := range []string{"", "exemplo.com", "app.exemplo.com.br", "a-b.xn--p1ai"} {
		if Domain(d) != nil {
			t.Errorf("%q deveria ser válido", d)
		}
	}
	for _, d := range []string{"localhost", "1.2.3.4", "*.exemplo.com", "Exemplo.com", "a b.com", "exemplo.com;x", "-a.com", "a.com:80", "a..com"} {
		if Domain(d) == nil {
			t.Errorf("%q deveria ser inválido", d)
		}
	}
}

func TestValidateScale(t *testing.T) {
	for _, tc := range []struct {
		r, m, c, l int
		ok         bool
	}{
		{1, 0, 0, 0, true}, {16, 512, 200, 100, true}, {1, 32, 5, 1, true},
		{0, 0, 0, 0, false}, {17, 0, 0, 0, false}, {-1, 0, 0, 0, false},
		{1, 31, 0, 0, false}, {1, 0, 4, 0, false}, {1, 0, 6401, 0, false}, {1, 0, 0, -1, false}, {1, 0, 0, 10001, false},
	} {
		if err := Scale(tc.r, tc.m, tc.c, tc.l); (err == nil) != tc.ok {
			t.Errorf("Scale(%d,%d,%d,%d) = %v", tc.r, tc.m, tc.c, tc.l, err)
		}
	}
}
