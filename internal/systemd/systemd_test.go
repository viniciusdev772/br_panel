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

func TestRenderResourceLimits(t *testing.T) {
	u := Unit{App: "w", StartCmd: "x", MemoryMB: 512, CPUPercent: 150, NoFile: 65536}.Render()
	for _, want := range []string{"MemoryMax=512M", "MemoryHigh=460M", "CPUQuota=150%", "LimitNOFILE=65536"} {
		if !strings.Contains(u, want) {
			t.Errorf("faltou %q em:\n%s", want, u)
		}
	}
	plain := Unit{App: "w", StartCmd: "x"}.Render()
	for _, bad := range []string{"MemoryMax", "MemoryHigh", "CPUQuota", "LimitNOFILE"} {
		if strings.Contains(plain, bad) {
			t.Errorf("sem limites configurados não deve haver %q", bad)
		}
	}
}

func TestRenderBindPaths(t *testing.T) {
	u := Unit{App: "w", StartCmd: "x",
		BindPaths:         []string{"/d/volumes/w/up:/d/apps/w/src/up", "/srv/m:/srv/m"},
		BindReadOnlyPaths: []string{"/d/files/w/3:/d/apps/w/src/app.json"}}.Render()
	for _, want := range []string{
		"BindPaths=/d/volumes/w/up:/d/apps/w/src/up\n", "BindPaths=/srv/m:/srv/m\n",
		"BindReadOnlyPaths=/d/files/w/3:/d/apps/w/src/app.json\n"} {
		if !strings.Contains(u, want) {
			t.Errorf("faltou %q em:\n%s", want, u)
		}
	}
	service := u[strings.Index(u, "[Service]"):strings.Index(u, "[Install]")]
	if !strings.Contains(service, "BindPaths=") || strings.Contains(u[:strings.Index(u, "[Service]")], "BindPaths") {
		t.Error("as montagens pertencem a [Service]")
	}
	if plain := (Unit{App: "w", StartCmd: "x"}).Render(); strings.Contains(plain, "Bind") {
		t.Errorf("sem montagens não deve haver Bind:\n%s", plain)
	}
}

func TestReplicaUnitNames(t *testing.T) {
	if ReplicaUnitName("web", 0) != UnitName("web") || ReplicaUnitName("web", 2) != "brpanelr2-web.service" {
		t.Error("nomes das instâncias")
	}
	if got := ReplicaUnits("web", 3); len(got) != 3 || got[0] != UnitName("web") || got[2] != "brpanelr2-web.service" {
		t.Errorf("%v", got)
	}
	if len(ReplicaUnits("web", 0)) != 1 {
		t.Error("sem contagem há uma instância")
	}
	// the replica prefix never equals an app unit: those always start with "brpanel-"
	seen := map[string]bool{UnitName("r2-web"): true, UnitName("web"): true}
	if seen[ReplicaUnitName("web", 2)] {
		t.Error("a unit da instância 2 de web não pode colidir com a app r2-web")
	}
}
