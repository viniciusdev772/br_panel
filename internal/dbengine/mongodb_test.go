package dbengine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/store"
)

// fakeMongo installs stand-ins for the MongoDB tools that record their arguments and the JavaScript
// they are asked to run, so the glue code can be tested without a MongoDB server.
func fakeMongo(t *testing.T) (logDir string) {
	t.Helper()
	bin := t.TempDir()
	logDir = t.TempDir()
	script := `#!/bin/sh
echo "$0 $@" >> "` + logDir + `/args.log"
for a in "$@"; do f="${a#--config=}"; case "$f" in *.js|*.yaml) cp "$f" "` + logDir + `/last-input" ;; esac; done
case "$0" in *mongosh) cat "` + logDir + `/reply" 2>/dev/null;; esac
exit 0
`
	for _, n := range []string{"mongod", "mongosh", "mongodump", "mongorestore"} {
		if err := os.WriteFile(filepath.Join(bin, n), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	return logDir
}

func mongoInst(t *testing.T) Instance {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "run"), 0o750)
	return Instance{Database: store.Database{Name: "docs", Kind: "mongodb", Port: 27018, Username: "admin", Password: "S3cretPw", DBName: "app"}, Dir: dir}
}

func TestMongoCreateUserKeepsPasswordOutOfArgv(t *testing.T) {
	logDir := fakeMongo(t)
	e := mongoEngine{}
	b, err := e.locate()
	if err != nil {
		t.Fatal(err)
	}
	inst := mongoInst(t)
	if err := e.postStart(context.Background(), runner{}, inst, b, os.Stderr); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(logDir, "args.log"))
	if strings.Contains(string(args), "S3cretPw") || !strings.Contains(string(args), "--nodb") {
		t.Errorf("a senha não pode ir no argv: %s", args)
	}
	js, _ := os.ReadFile(filepath.Join(logDir, "last-input"))
	for _, want := range []string{`createUser`, `"admin"`, `S3cretPw`, `role: "root"`, `mongodb://127.0.0.1:27018/`} {
		if !strings.Contains(string(js), want) {
			t.Errorf("script sem %q:\n%s", want, js)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(inst.RunDir(), "eval-*.js")); len(left) != 0 {
		t.Error("o script temporário com a senha deve ser apagado")
	}
}

func TestMongoBackupUsesConfigFile(t *testing.T) {
	logDir := fakeMongo(t)
	e := mongoEngine{}
	b, _ := e.locate()
	inst := mongoInst(t)
	if err := e.backup(context.Background(), runner{}, inst, b, filepath.Join(inst.Dir, "x.archive.gz"), os.Stderr); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(logDir, "args.log"))
	if strings.Contains(string(args), "S3cretPw") || !strings.Contains(string(args), "--archive=") || !strings.Contains(string(args), "--gzip") {
		t.Errorf("%s", args)
	}
	cfg, _ := os.ReadFile(filepath.Join(logDir, "last-input"))
	if !strings.Contains(string(cfg), "mongodb://admin:S3cretPw@127.0.0.1:27018/app?authSource=admin") {
		t.Errorf("config: %s", cfg)
	}
}

func TestMongoPreviewAndCollectionNames(t *testing.T) {
	logDir := fakeMongo(t)
	e := mongoEngine{}
	b, _ := e.locate()
	inst := mongoInst(t)
	_ = os.WriteFile(filepath.Join(logDir, "reply"), []byte("aviso qualquer\n"+`["usuarios","pedidos"]`+"\n"), 0o644)
	names, err := e.tables(context.Background(), runner{}, inst, b)
	if err != nil || len(names) != 2 || names[0] != "usuarios" {
		t.Fatalf("%v %v", names, err)
	}
	_ = os.WriteFile(filepath.Join(logDir, "reply"), []byte(`{"total":2,"docs":[{"_id":"a","nome":"Ana"},{"_id":"b","idade":30,"nome":"Bia","tags":["x"]}]}`+"\n"), 0o644)
	pv, err := e.preview(context.Background(), runner{}, inst, b, "usuarios")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(pv.Columns, ",") != "_id,idade,nome,tags" || pv.Total != 2 || pv.Rows[0][1] != "NULL" || pv.Rows[1][1] != "30" || pv.Rows[1][3] != `["x"]` {
		t.Errorf("%+v", pv)
	}
	for _, bad := range []string{`x"); db.dropDatabase(); //`, "a b", "$cmd", "", strings.Repeat("a", 80)} {
		if _, err := e.preview(context.Background(), runner{}, inst, b, bad); err == nil {
			t.Errorf("coleção %q deveria ser recusada", bad)
		}
	}
}
