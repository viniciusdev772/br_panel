package dbengine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type mongoEngine struct{}

type mongoBins struct{ server, shell, dump, restore string }

func (mongoEngine) locate() (any, error) {
	b := mongoBins{
		server:  which([]string{"/usr/local/bin"}, "mongod"),
		shell:   which([]string{"/usr/local/bin"}, "mongosh"),
		dump:    which([]string{"/usr/local/bin"}, "mongodump"),
		restore: which([]string{"/usr/local/bin"}, "mongorestore"),
	}
	return b, missingList(map[string]string{"mongod": b.server, "mongosh": b.shell, "mongodump": b.dump, "mongorestore": b.restore})
}

func (mongoEngine) version(b any) string {
	out, err := runner{}.out(context.Background(), nil, b.(mongoBins).server, "--version")
	if err != nil {
		return ""
	}
	return firstVersion(strings.SplitN(out, "\n", 2)[0])
}

func (mongoEngine) restoreOffline() bool { return false }
func (mongoEngine) backupExt() string    { return "archive.gz" }
func (mongoEngine) extraUnit() []string  { return []string{"TimeoutStopSec=120", "LimitNOFILE=64000"} }

func (mongoEngine) init(context.Context, runner, Instance, any, io.Writer) error { return nil }
func (mongoEngine) configure(Instance, any) error                                { return nil }

func (mongoEngine) execStart(inst Instance, b any) string {
	return fmt.Sprintf("%s --dbpath %s --port %d --bind_ip %s --auth --unixSocketPrefix %s --pidfilepath %s --wiredTigerCacheSizeGB 0.5",
		b.(mongoBins).server, inst.DataDir(), inst.Port, inst.BindAddr(), inst.RunDir(), filepath.Join(inst.RunDir(), "mongod.pid"))
}

func (mongoEngine) url(inst Instance, host string) string {
	return fmt.Sprintf("mongodb://%s:%s@%s:%d/%s?authSource=admin", inst.Username, inst.Password, host, inst.Port, inst.DBName)
}

// eval runs JavaScript in mongosh. The connection string (with the password) lives only in a
// temporary 0600 file, never in argv. body can use `db` (the app database) and `conn`.
func (mongoEngine) eval(ctx context.Context, r runner, inst Instance, b any, uri, body string) (string, error) {
	js := fmt.Sprintf("const conn = new Mongo(%s);\nconst db = conn.getDB(%s);\n%s\n", jsonStr(uri), jsonStr(inst.DBName), body)
	f, err := os.CreateTemp(inst.RunDir(), "eval-*.js")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_ = f.Chmod(0o600)
	if _, err := f.WriteString(js); err != nil {
		f.Close()
		return "", err
	}
	f.Close()
	return r.out(ctx, nil, b.(mongoBins).shell, "--nodb", "--quiet", "--file", f.Name())
}

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }

func (e mongoEngine) authURI(inst Instance) string { return e.url(inst, "127.0.0.1") }

// postStart creates the root user through the localhost exception (auth is on, no user exists yet).
func (e mongoEngine) postStart(ctx context.Context, r runner, inst Instance, b any, log io.Writer) error {
	fmt.Fprintf(log, "$ mongosh (criando o usuário %s)\n", inst.Username)
	noAuth := fmt.Sprintf("mongodb://127.0.0.1:%d/", inst.Port)
	_, err := e.eval(ctx, r, inst, b, noAuth, fmt.Sprintf(
		`try { conn.getDB("admin").createUser({user: %s, pwd: %s, roles: [{role: "root", db: "admin"}]}); }
catch (e) { if (!/already exists/i.test(String(e))) throw e; }`, jsonStr(inst.Username), jsonStr(inst.Password)))
	return err
}

// configFile writes the YAML accepted by mongodump/mongorestore (--config) so the URI stays out of argv.
func (e mongoEngine) configFile(inst Instance) (string, error) {
	f, err := os.CreateTemp(inst.RunDir(), "cfg-*.yaml")
	if err != nil {
		return "", err
	}
	_ = f.Chmod(0o600)
	_, err = fmt.Fprintf(f, "uri: %s\n", jsonStr(e.authURI(inst)))
	f.Close()
	return f.Name(), err
}

func (e mongoEngine) backup(ctx context.Context, r runner, inst Instance, b any, dst string, log io.Writer) error {
	cfg, err := e.configFile(inst)
	if err != nil {
		return err
	}
	defer os.Remove(cfg)
	return r.run(ctx, log, false, nil, b.(mongoBins).dump, "--config="+cfg, "--archive="+dst, "--gzip")
}

func (e mongoEngine) restore(ctx context.Context, r runner, inst Instance, b any, src string, log io.Writer) error {
	cfg, err := e.configFile(inst)
	if err != nil {
		return err
	}
	defer os.Remove(cfg)
	return r.run(ctx, log, false, nil, b.(mongoBins).restore, "--config="+cfg, "--archive="+src, "--gzip", "--drop")
}

func (e mongoEngine) setPassword(ctx context.Context, r runner, inst Instance, b any, newPassword string) error {
	_, err := e.eval(ctx, r, inst, b, e.authURI(inst),
		fmt.Sprintf(`conn.getDB("admin").changeUserPassword(%s, %s);`, jsonStr(inst.Username), jsonStr(newPassword)))
	return err
}

func (e mongoEngine) tables(ctx context.Context, r runner, inst Instance, b any) ([]string, error) {
	out, err := e.eval(ctx, r, inst, b, e.authURI(inst), `print(JSON.stringify(db.getCollectionNames()));`)
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal([]byte(lastNonEmpty(out)), &names); err != nil {
		return nil, err
	}
	return names, nil
}

var collRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)

func lastNonEmpty(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func (e mongoEngine) preview(ctx context.Context, r runner, inst Instance, b any, name string) (Preview, error) {
	if !collRe.MatchString(name) {
		return Preview{}, fmt.Errorf("nome de coleção inválido")
	}
	out, err := e.eval(ctx, r, inst, b, e.authURI(inst), fmt.Sprintf(
		`const c = db.getCollection(%s);
print(JSON.stringify({total: c.estimatedDocumentCount(), docs: c.find({}).limit(50).toArray()}));`, jsonStr(name)))
	if err != nil {
		return Preview{}, err
	}
	var res struct {
		Total int              `json:"total"`
		Docs  []map[string]any `json:"docs"`
	}
	if err := json.Unmarshal([]byte(lastNonEmpty(out)), &res); err != nil {
		return Preview{}, err
	}
	p := Preview{Total: res.Total}
	seen := map[string]bool{}
	for _, d := range res.Docs { // columns: union of keys, `_id` first, then sorted
		for k := range d {
			if !seen[k] {
				seen[k] = true
				p.Columns = append(p.Columns, k)
			}
		}
	}
	sortColumns(p.Columns)
	for _, d := range res.Docs {
		line := make([]string, len(p.Columns))
		for i, c := range p.Columns {
			line[i] = cell(d[c])
		}
		p.Rows = append(p.Rows, line)
	}
	return p, nil
}

func sortColumns(cols []string) {
	for i := 1; i < len(cols); i++ { // insertion sort: "_id" first, then alphabetical
		for j := i; j > 0 && colLess(cols[j], cols[j-1]); j-- {
			cols[j], cols[j-1] = cols[j-1], cols[j]
		}
	}
}

func colLess(a, b string) bool {
	if a == "_id" {
		return b != "_id"
	}
	if b == "_id" {
		return false
	}
	return a < b
}
