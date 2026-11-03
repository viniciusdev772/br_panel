package validate

import "testing"

func TestRootDir(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""}, {".", ""}, {"./", ""}, {"  apps/web  ", "apps/web"}, {"./apps/web/", "apps/web"}, {"packages/@ui", ""},
		{"apps/web", "apps/web"}, {"services/api-v2", "services/api-v2"}, {".config/app", ".config/app"}, {"a_b/c.d", "a_b/c.d"},
	} {
		got, err := RootDir(c.in)
		if c.in == "packages/@ui" {
			if err == nil {
				t.Errorf("RootDir(%q) deveria falhar (@ não é aceito)", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("RootDir(%q) = %q, %v; quero %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{
		"..", "../x", "apps/../etc", "apps/..", "/etc", "/apps/web", `apps\web`, "apps//web", "apps/./web", ".git", "apps/.git", "a/.GIT/b",
		"-rf", "apps/-x", "apps/web; rm -rf /", "apps/$(id)", "apps/`id`", "apps/web\nx", "apps/we b", "apps/\x00", "apps/é",
		"a/b/c/d/e/f/g/h/i/j/k",
	} {
		if got, err := RootDir(bad); err == nil {
			t.Errorf("RootDir(%q) = %q deveria falhar", bad, got)
		}
	}
}

func TestWatchPaths(t *testing.T) {
	got, err := WatchPaths("packages/ui/**\n ./packages/config , libs/*/src\n\n")
	if err != nil || got != "packages/ui/**\npackages/config\nlibs/*/src" {
		t.Errorf("WatchPaths = %q, %v", got, err)
	}
	if got, err := WatchPaths("packages/**\n!packages/docs/**"); err != nil || got != "packages/**\n!packages/docs/**" {
		t.Errorf("exclusão: %q, %v", got, err)
	}
	if got, err := WatchPaths("  \n "); err != nil || got != "" {
		t.Errorf("vazio: %q %v", got, err)
	}
	for _, bad := range []string{"/etc/**", "../x", "a/../b", "a;b", "a b", "$(id)", "a`b`", `a\b`, "!", "!!x", "!/etc", "{a,b}"} {
		if got, err := WatchPaths(bad); err == nil {
			t.Errorf("WatchPaths(%q) = %q deveria falhar", bad, got)
		}
	}
	many := ""
	for i := 0; i < 21; i++ {
		many += "p/x\n"
	}
	if _, err := WatchPaths(many); err == nil {
		t.Error("mais de 20 caminhos deveria falhar")
	}
}
