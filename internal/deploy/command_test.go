package deploy

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/githubapp"
	"github.com/viniciusdev772/br_panel/internal/settings"
)

func deployerAs(user string) *Deployer {
	set := settings.Default()
	set.AppUser = user
	return New(nil, "/dados", func() settings.Settings { return set }, func() *githubapp.Client { return nil })
}

// Regression: runuser without -m overwrites HOME with /home/<user>, which does not exist for
// the system user the panel creates, so `npm ci` died with "EACCES: mkdir '/home/brapps'".
func TestCommandKeepsTheEnvironmentWhenDroppingPrivileges(t *testing.T) {
	c := deployerAs("brapps").command(context.Background(), "/src", []string{"HOME=/dados/apps/x"}, "npm", "ci")
	want := []string{"runuser", "-u", "brapps", "-m", "--", "npm", "ci"}
	if !reflect.DeepEqual(c.Args, want) {
		t.Errorf("args = %v, want %v", c.Args, want)
	}
	if len(c.Env) != 1 || c.Env[0] != "HOME=/dados/apps/x" {
		t.Errorf("env = %v", c.Env)
	}
	if c := deployerAs("").command(context.Background(), "/src", nil, "npm", "ci"); !reflect.DeepEqual(c.Args, []string{"npm", "ci"}) {
		t.Errorf("sem usuário das apps o comando roda direto: %v", c.Args)
	}
}

// The real runuser must hand HOME, PORT and HOST to the build (skipped without root/runuser).
func TestCommandPassesHomeToTheBuildAsAppUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("precisa de root")
	}
	if _, err := os.Stat("/usr/sbin/runuser"); err != nil {
		if _, err := os.Stat("/sbin/runuser"); err != nil {
			t.Skip("sem runuser")
		}
	}
	user := os.Getenv("BP_TEST_USER") // an existing unprivileged account
	if user == "" {
		t.Skip("defina BP_TEST_USER")
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0o755); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	d := deployerAs(user)
	c := d.command(context.Background(), filepath.Dir(home), []string{"PATH=" + execPath(), "HOME=" + home, "PORT=3000"}, "sh", "-c", "echo $HOME $PORT")
	c.Stdout = &out
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != home+" 3000" {
		t.Errorf("o build recebeu %q, esperado HOME e PORT do deploy", got)
	}
}
