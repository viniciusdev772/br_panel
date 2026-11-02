// Package systemd renders unit files and wraps systemctl/journalctl for the panel's app services.
package systemd

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/viniciusdev772/br_panel/internal/sysinfo"
)

// UnitName is the systemd unit that runs an app.
func UnitName(app string) string { return "brpanel-" + app + ".service" }

// ReplicaUnitName is the unit of instance k of an app. Instance 0 keeps the plain app unit, so
// single-instance apps are unchanged. The "brpanelr<k>-" prefix cannot collide with an app unit
// (those start with "brpanel-") nor with a database unit.
//
// Replicas are separate generated units, not a systemd template (foo@.service): the panel needs a
// different PORT per instance, and systemctl globs only match units that are already loaded, so
// the panel enumerates the instances itself anyway.
func ReplicaUnitName(app string, k int) string {
	if k <= 0 {
		return UnitName(app)
	}
	return "brpanelr" + strconv.Itoa(k) + "-" + app + ".service"
}

// ReplicaUnits lists the units of an app with n instances.
func ReplicaUnits(app string, n int) []string {
	if n < 1 {
		n = 1
	}
	units := make([]string, n)
	for k := range units {
		units[k] = ReplicaUnitName(app, k)
	}
	return units
}

// DBUnitName is the systemd unit that runs a managed database. The prefix has no hyphen after
// "brpanel", so it can never collide with an app unit (brpanel-<app>.service).
func DBUnitName(db string) string { return "brpaneldb-" + db + ".service" }

// Escape escapes a string for use inside a double-quoted ExecStart argument.
func Escape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`)
	return r.Replace(s)
}

var leadingAssignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// shellLine is the argument of `sh -c`. A plain command gets `exec`, so the program becomes the
// service's main process. `exec` is wrong in two cases: a leading VAR=value (exec takes no
// assignments; `exec env` does the same job) and compound commands such as `a && b`, where
// `exec a` would replace the shell and `b` would never run.
func shellLine(cmd string) string {
	switch {
	case strings.ContainsAny(cmd, "&|;<>()`\n"):
		return cmd
	case leadingAssignRe.MatchString(cmd):
		return "exec env " + cmd
	}
	return "exec " + cmd
}

// EnvFileLine renders KEY="value" for a systemd EnvironmentFile.
func EnvFileLine(k, v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return fmt.Sprintf("%s=\"%s\"\n", k, r.Replace(v))
}

// Unit describes the service to render.
type Unit struct {
	App      string
	Desc     string // overrides the default description
	WorkDir  string
	EnvFile  string // optional
	StartCmd string
	User     string   // empty = root
	Extra    []string // additional [Service] lines, e.g. "TimeoutStopSec=120"

	// Resource limits of this instance (apps only); zero leaves them unset.
	MemoryMB   int // MemoryMax; MemoryHigh is set to 90% of it so the kernel throttles before it kills
	CPUPercent int // CPUQuota, relative to one CPU (200 = two CPUs)
	NoFile     int // LimitNOFILE
}

// Render returns the unit file contents.
func (u Unit) Render() string {
	var b strings.Builder
	desc := u.Desc
	if desc == "" {
		desc = "BR Panel app " + u.App
	}
	fmt.Fprintf(&b, "[Unit]\nDescription=%s\nAfter=network.target\n\n", desc)
	b.WriteString("[Service]\n")
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", u.WorkDir)
	if u.EnvFile != "" {
		fmt.Fprintf(&b, "EnvironmentFile=%s\n", u.EnvFile)
	}
	fmt.Fprintf(&b, "ExecStart=/bin/sh -c \"%s\"\n", Escape(shellLine(u.StartCmd)))
	b.WriteString("Restart=on-failure\nRestartSec=3\n")
	if u.User != "" {
		fmt.Fprintf(&b, "User=%s\n", u.User)
	}
	for _, l := range u.Extra {
		b.WriteString(l + "\n")
	}
	if u.MemoryMB > 0 {
		fmt.Fprintf(&b, "MemoryHigh=%dM\nMemoryMax=%dM\n", u.MemoryMB*9/10, u.MemoryMB)
	}
	if u.CPUPercent > 0 {
		fmt.Fprintf(&b, "CPUQuota=%d%%\n", u.CPUPercent)
	}
	if u.NoFile > 0 {
		fmt.Fprintf(&b, "LimitNOFILE=%d\n", u.NoFile)
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

// State returns the app unit's ActiveState ("active", "failed", …) or "unknown".
func State(app string) string { return StateUnit(UnitName(app)) }

// StateUnit is State for an arbitrary unit name.
func StateUnit(unit string) string {
	out, _ := Ctl("is-active", unit)
	if out == "" {
		return "unknown"
	}
	return strings.Fields(out)[0]
}

// StateUnits summarizes several units: "active" when all run, the state of the first when none
// does, and "degraded" when only some do.
func StateUnits(units []string) string {
	active := 0
	for _, u := range units {
		if StateUnit(u) == "active" {
			active++
		}
	}
	switch {
	case active == len(units):
		return "active"
	case active == 0:
		return StateUnit(units[0])
	}
	return "degraded"
}

// JournalUnits merges the last n journal lines of several units.
func JournalUnits(units []string, lines int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := []string{"-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso"}
	for _, u := range units {
		args = append(args, "-u", u)
	}
	out, err := exec.CommandContext(ctx, "journalctl", args...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return "journalctl indisponível: " + err.Error()
	}
	return string(out)
}

// Journal returns the last n journal lines of the app's unit.
func Journal(app string, lines int) string { return JournalUnit(UnitName(app), lines) }

// JournalUnit is Journal for an arbitrary unit name.
func JournalUnit(unit string, lines int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "journalctl", "-u", unit, "-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso").CombinedOutput()
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

func Info(app string) ServiceInfo { return InfoUnit(UnitName(app)) }

// InfoAll is Info for an app with several instances: the state and uptime of the first instance,
// with the restart count summed over all of them.
func InfoAll(units []string) ServiceInfo {
	info := InfoUnit(units[0])
	for _, u := range units[1:] {
		info.Restarts += InfoUnit(u).Restarts
	}
	return info
}

// InfoUnit is Info for an arbitrary unit name.
func InfoUnit(unit string) ServiceInfo {
	out, _ := Ctl("show", unit, "-p", "ActiveState", "-p", "SubState", "-p", "NRestarts",
		"-p", "MainPID", "-p", "ActiveEnterTimestampMonotonic")
	return ParseShow(out, sysinfo.Uptime())
}
