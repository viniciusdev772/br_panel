package dbengine

import (
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/store"
)

var testDBs = []store.Database{
	{Name: "pg", Kind: "postgres", Port: 5433, Username: "postgres", Password: "s3cr3t", DBName: "app"},
	{Name: "cache", Kind: "redis", Port: 6380, Username: "default", Password: "r3d1s"},
	{Name: "docs", Kind: "mongodb", Port: 27018, Username: "admin", Password: "m0ng0", DBName: "app"},
	{Name: "my", Kind: "mysql", Port: 3307, Username: "root", Password: "my5ql", DBName: "app"},
}

func TestVarsAndURLs(t *testing.T) {
	want := map[string]string{
		"pg.DATABASE_URL":  "postgresql://postgres:s3cr3t@127.0.0.1:5433/app",
		"pg.URL":           "postgresql://postgres:s3cr3t@127.0.0.1:5433/app",
		"pg.PORT":          "5433",
		"pg.PASSWORD":      "s3cr3t",
		"cache.REDIS_URL":  "redis://default:r3d1s@127.0.0.1:6380",
		"docs.MONGO_URL":   "mongodb://admin:m0ng0@127.0.0.1:27018/app?authSource=admin",
		"docs.MONGODB_URI": "mongodb://admin:m0ng0@127.0.0.1:27018/app?authSource=admin",
		"my.DATABASE_URL":  "mysql://root:my5ql@127.0.0.1:3307/app",
		"my.DATABASE":      "app",
	}
	for ref, exp := range want {
		got, unknown := Resolve("${{"+ref+"}}", testDBs)
		if got != exp || len(unknown) != 0 {
			t.Errorf("%s => %q (desconhecidas %v), esperado %q", ref, got, unknown, exp)
		}
	}
	// whitespace inside the braces is accepted; surrounding text is kept
	if got, _ := Resolve("a=${{ pg.HOST }}:${{pg.PORT}}", testDBs); got != "a=127.0.0.1:5433" {
		t.Errorf("%q", got)
	}
}

func TestResolveUnknown(t *testing.T) {
	out, unknown := Resolve("X=${{nope.URL}} Y=${{pg.NOPE}}", testDBs)
	if out != "X=${{nope.URL}} Y=${{pg.NOPE}}" || len(unknown) != 2 {
		t.Errorf("%q %v", out, unknown)
	}
	if err := CheckRefs("A=${{nope.URL}}", testDBs); err == nil || !strings.Contains(err.Error(), "nope.URL") {
		t.Errorf("%v", err)
	}
	if CheckRefs("A=1\nB=${{pg.URL}}", testDBs) != nil {
		t.Error("referência válida")
	}
}

func TestReferencesAndVarNames(t *testing.T) {
	refs := References("A=${{pg.URL}}\nB=${{cache.REDIS_URL}}\nC=${{pg.PORT}}")
	if len(refs) != 2 || refs[0] != "pg" || refs[1] != "cache" {
		t.Errorf("%v", refs)
	}
	names := VarNames(testDBs[0])
	if names[0] != "URL" || len(names) < 6 {
		t.Errorf("%v", names)
	}
}

func TestGeneratePassword(t *testing.T) {
	a, b := GeneratePassword(), GeneratePassword()
	if len(a) != 24 || a == b {
		t.Errorf("%q %q", a, b)
	}
	for _, c := range a {
		if !strings.ContainsRune(alnum, c) {
			t.Errorf("caractere inesperado %q", c)
		}
	}
}
