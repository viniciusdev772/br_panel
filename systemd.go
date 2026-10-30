package main

import (
	"fmt"
	"strings"
)

func unitName(app string) string { return "brpanel-" + app + ".service" }

// systemdEscape escapes a string for use inside a double-quoted ExecStart argument.
func systemdEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`)
	return r.Replace(s)
}

// envFileLine renders KEY="value" for a systemd EnvironmentFile.
func envFileLine(k, v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return fmt.Sprintf("%s=\"%s\"\n", k, r.Replace(v))
}

func renderUnit(a App, p Paths, appUser string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Unit]\nDescription=BR Panel app %s\nAfter=network.target\n\n", a.Name)
	b.WriteString("[Service]\n")
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", p.SrcDir(a.Name))
	fmt.Fprintf(&b, "EnvironmentFile=%s\n", p.EnvFile(a.Name))
	fmt.Fprintf(&b, "ExecStart=/bin/sh -c \"exec %s\"\n", systemdEscape(a.StartCmd))
	b.WriteString("Restart=on-failure\nRestartSec=3\n")
	if appUser != "" {
		fmt.Fprintf(&b, "User=%s\n", appUser)
	}
	b.WriteString("CPUAccounting=yes\nMemoryAccounting=yes\n")
	b.WriteString("NoNewPrivileges=true\nPrivateTmp=true\nProtectSystem=full\n\n")
	b.WriteString("[Install]\nWantedBy=multi-user.target\n")
	return b.String()
}
