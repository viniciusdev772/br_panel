package systemd

import (
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
