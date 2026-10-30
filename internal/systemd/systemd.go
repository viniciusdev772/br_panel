// Package systemd renders unit files and wraps systemctl/journalctl for the panel's app services.
package systemd

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/viniciusdev772/br_panel/internal/sysinfo"
)

// UnitName is the systemd unit that runs an app.
func UnitName(app string) string { return "brpanel-" + app + ".service" }

// Escape escapes a string for use inside a double-quoted ExecStart argument.
func Escape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`)
	return r.Replace(s)
}

// EnvFileLine renders KEY="value" for a systemd EnvironmentFile.
func EnvFileLine(k, v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return fmt.Sprintf("%s=\"%s\"\n", k, r.Replace(v))
}

// Unit describes the service to render.
type Unit struct {
	App      string
	WorkDir  string
	EnvFile  string
	StartCmd string
	User     string // empty = root
}

// Render returns the unit file contents.
func (u Unit) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Unit]\nDescription=BR Panel app %s\nAfter=network.target\n\n", u.App)
	b.WriteString("[Service]\n")
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", u.WorkDir)
	fmt.Fprintf(&b, "EnvironmentFile=%s\n", u.EnvFile)
	fmt.Fprintf(&b, "ExecStart=/bin/sh -c \"exec %s\"\n", Escape(u.StartCmd))
	b.WriteString("Restart=on-failure\nRestartSec=3\n")
	if u.User != "" {
		fmt.Fprintf(&b, "User=%s\n", u.User)
	}
	b.WriteString("CPUAccounting=yes\nMemoryAccounting=yes\n")
	b.WriteString("NoNewPrivileges=true\nPrivateTmp=true\nProtectSystem=full\n\n")
	b.WriteString("[Install]\nWantedBy=multi-user.target\n")
	return b.String()
}

// Ctl runs systemctl and returns its trimmed combined output.
func Ctl(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// State returns the unit's ActiveState ("active", "failed", …) or "unknown".
func State(app string) string {
	out, _ := Ctl("is-active", UnitName(app))
	if out == "" {
		return "unknown"
	}
	return strings.Fields(out)[0]
}

// Journal returns the last n journal lines of the app's unit.
func Journal(app string, lines int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "journalctl", "-u", UnitName(app), "-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso").CombinedOutput()
	if err != nil && len(out) == 0 {
		return "journalctl indisponível: " + err.Error()
	}
	return string(out)
}

// ServiceInfo is the runtime status of an app service.
type ServiceInfo struct {
	State    string `json:"state"`
	SubState string `json:"sub_state"`
	Restarts int    `json:"restarts"`
	Uptime   int64  `json:"uptime"` // seconds, 0 when not running
	PID      int    `json:"pid"`
}

func ParseShow(out string, upSeconds float64) ServiceInfo {
	kv := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			kv[k] = strings.TrimSpace(v)
		}
	}
	info := ServiceInfo{State: kv["ActiveState"], SubState: kv["SubState"]}
	if info.State == "" {
		info.State = "unknown"
	}
	info.Restarts, _ = strconv.Atoi(kv["NRestarts"])
	info.PID, _ = strconv.Atoi(kv["MainPID"])
	if mono, _ := strconv.ParseFloat(kv["ActiveEnterTimestampMonotonic"], 64); mono > 0 && info.State == "active" {
		if u := int64(upSeconds - mono/1e6); u > 0 {
			info.Uptime = u
		}
	}
	return info
}

func Info(app string) ServiceInfo {
	out, _ := Ctl("show", UnitName(app), "-p", "ActiveState", "-p", "SubState", "-p", "NRestarts",
		"-p", "MainPID", "-p", "ActiveEnterTimestampMonotonic")
	return ParseShow(out, sysinfo.Uptime())
}
