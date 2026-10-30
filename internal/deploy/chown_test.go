package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBin puts scripts named after the commands first in PATH and returns the call log path.
func fakeBin(t *testing.T, scripts map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return filepath.Join(dir, "calls")
}

// Regression: chown ran through Deployer.run, which wraps commands in `runuser -u <app user>`;
// an unprivileged chown always fails, so detection and the first deploy broke whenever an
// app user was configured.
func TestChownRunsAsThePanelNotAsTheAppUser(t *testing.T) {
	calls := fakeBin(t, map[string]string{
		"chown":   `echo "chown $@" >> "$(dirname "$0")/calls"`,
		"runuser": `echo "runuser $@" >> "$(dirname "$0")/calls"; exit 1`,
	})
	if err := chownTo(context.Background(), "brapps", "/dados/tmp/detect-1"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(calls)
	if strings.TrimSpace(string(got)) != "chown brapps: /dados/tmp/detect-1" {
		t.Errorf("chamadas = %q", got)
	}
}

func TestChownErrorKeepsTheCommandMessage(t *testing.T) {
	fakeBin(t, map[string]string{"chown": `echo "chown: invalid user: 'ninguem'" >&2; exit 1`})
	err := chownTo(context.Background(), "ninguem", "/x")
	if err == nil || !strings.Contains(err.Error(), "invalid user") || !strings.Contains(err.Error(), "ninguem") {
		t.Errorf("err = %v", err)
	}
}
