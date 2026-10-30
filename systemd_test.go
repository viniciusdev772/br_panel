package main

import (
	"strings"
	"testing"
)

func TestSystemdEscape(t *testing.T) {
	got := systemdEscape(`node a.js --x="$HOME" 100%`)
	want := `node a.js --x=\"$$HOME\" 100%%`
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestRenderUnit(t *testing.T) {
	u := renderUnit(App{Name: "web", StartCmd: "npm start"}, Paths{Data: "/d", Units: "/u"}, "app")
	for _, s := range []string{"WorkingDirectory=/d/apps/web/src", "EnvironmentFile=/d/env/web.env", `ExecStart=/bin/sh -c "exec npm start"`, "User=app"} {
		if !strings.Contains(u, s) {
			t.Errorf("faltou %q em:\n%s", s, u)
		}
	}
}
