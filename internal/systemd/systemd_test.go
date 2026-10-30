package systemd

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestEscape(t *testing.T) {
	got := Escape(`node a.js --x="$HOME" 100%`)
	want := `node a.js --x=\"$$HOME\" 100%%`
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestRender(t *testing.T) {
	u := Unit{App: "web", WorkDir: "/d/apps/web/src", EnvFile: "/d/env/web.env", StartCmd: "npm start", User: "app"}.Render()
	for _, s := range []string{"WorkingDirectory=/d/apps/web/src", "EnvironmentFile=/d/env/web.env", `ExecStart=/bin/sh -c "exec npm start"`, "User=app", "MemoryAccounting=yes", "CPUAccounting=yes"} {
		if !strings.Contains(u, s) {
			t.Errorf("faltou %q em:\n%s", s, u)
		}
	}
	if strings.Contains(Unit{App: "x", StartCmd: "y"}.Render(), "User=") {
		t.Error("sem usuário configurado não deve haver User=")
	}
}

func TestParseShow(t *testing.T) {
	out := "ActiveState=active\nSubState=running\nNRestarts=2\nMainPID=4121\nActiveEnterTimestampMonotonic=100000000\n"
	i := ParseShow(out, 400)
	if i.State != "active" || i.Restarts != 2 || i.PID != 4121 || i.Uptime != 300 {
		t.Errorf("%+v", i)
	}
	if i := ParseShow("", 10); i.State != "unknown" || i.Uptime != 0 {
		t.Errorf("%+v", i)
	}
}

func TestRenderDatabaseUnit(t *testing.T) {
	u := Unit{App: "pg", Desc: "BR Panel database pg", WorkDir: "/d/pg", StartCmd: "postgres -D /d/pg/data", User: "brdb",
		Extra: []string{"KillSignal=SIGINT", "TimeoutStopSec=120"}}.Render()
	for _, s := range []string{"Description=BR Panel database pg", "KillSignal=SIGINT", "TimeoutStopSec=120", "User=brdb"} {
		if !strings.Contains(u, s) {
			t.Errorf("faltou %q em:\n%s", s, u)
		}
	}
	if strings.Contains(u, "EnvironmentFile") {
		t.Error("sem EnvFile não deve haver EnvironmentFile")
	}
	if DBUnitName("pg") != "brpaneldb-pg.service" || DBUnitName("x") == UnitName("db-x") {
		t.Error("nome de unit de banco")
	}
}

func TestShellLine(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"npm start", "exec npm start"},
		{"node server.js --port $PORT", "exec node server.js --port $PORT"},
		{"NODE_ENV=production node index.js", "exec env NODE_ENV=production node index.js"},
		{"A=1 B=2 npm start", "exec env A=1 B=2 npm start"},
		{"npm run build && node dist/main.js", "npm run build && node dist/main.js"},
		{"FOO=1 a | b", "FOO=1 a | b"},
		{"a; b", "a; b"},
	} {
		if got := shellLine(tc.in); got != tc.want {
			t.Errorf("shellLine(%q) = %q, quer %q", tc.in, got, tc.want)
		}
	}
}

// Regression: "exec NODE_ENV=production node index.js" died with
// "exec: NODE_ENV=production: not found" (status 127) in a restart loop.
func TestShellLineRunsInARealShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil || runtime.GOOS == "windows" {
		t.Skip("sem sh")
	}
	for cmd, want := range map[string]string{
		"FOO=bar printenv FOO":              "bar",
		"echo antes && echo depois":         "antes\ndepois",
		"FOO=a BAR=b sh -c 'echo $FOO$BAR'": "ab",
	} {
		out, err := exec.Command("sh", "-c", shellLine(cmd)).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != want {
			t.Errorf("%q -> %q %v, quer %q", cmd, out, err, want)
		}
	}
	// And the unit line carries the same text, escaped for systemd.
	u := Unit{App: "x", StartCmd: "NODE_ENV=production node index.js"}.Render()
	if !strings.Contains(u, `ExecStart=/bin/sh -c "exec env NODE_ENV=production node index.js"`) {
		t.Errorf("unit:\n%s", u)
	}
}
