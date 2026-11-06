package ai

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/viniciusdev772/br_panel/internal/cli"
)

// Every command of the CLI catalog is decided here: free ones only read, confirm ones change the
// server and wait for the user, the rest are never offered to a model (secrets, login, self-update,
// long waits). TestEveryCommandIsClassified fails when a new command is not listed.
var (
	free = set("status", "events", "apps list", "apps show", "apps logs", "dbs list", "dbs show", "templates list", "templates show", "cache status")

	confirm = set("apps create", "apps set", "apps deploy", "apps start", "apps stop", "apps restart", "apps delete",
		"apps env-set", "apps env-unset", "apps domain-add", "apps domain-remove", "apps maintenance", "apps limits",
		"dbs create", "dbs start", "dbs stop", "dbs restart", "dbs backup", "dbs delete",
		"templates install", "cache prune", "cache clear")

	// withheld: they print or take secrets (env values, passwords, tokens, raw requests), act on the
	// program itself, or block for minutes.
	withheld = set("guide", "skills install", "version", "update", "help", "apps wait", "apps env", "apps auth-add", "apps auth-remove",
		"dbs regenerate", "login", "logout", "whoami", "tokens list", "tokens create", "tokens revoke", "raw")
)

func set(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// toolName is the command name made valid for all three APIs: "apps env-set" -> "apps_env-set".
func toolName(c cli.Cmd) string { return strings.ReplaceAll(c.Name, " ", "_") }

func offered(c cli.Cmd) bool { return free[c.Name] || confirm[c.Name] }

// Classified reports whether a command was decided on (used by the test).
func Classified(name string) bool { return free[name] || confirm[name] || withheld[name] }

// Tools builds one tool per offered command, with the JSON Schema of its positional arguments and flags.
func Tools() []Tool {
	var out []Tool
	for _, c := range cli.Catalog {
		if !offered(c) {
			continue
		}
		props := obj{}
		if c.Args != "" {
			props["args"] = obj{"type": "array", "items": obj{"type": "string"}, "description": "Argumentos posicionais, nesta ordem: " + c.Args}
		}
		for _, f := range c.Flags {
			switch {
			case f.Value == "":
				props[f.Name] = obj{"type": "boolean", "description": f.Desc}
			case repeats(f):
				props[f.Name] = obj{"type": "array", "items": obj{"type": "string"}, "description": f.Desc + " (" + f.Value + ")"}
			default:
				props[f.Name] = obj{"type": "string", "description": f.Desc + " (" + f.Value + ")"}
			}
		}
		schema := obj{"type": "object", "properties": props}
		if c.Min > 0 {
			schema["required"] = []string{"args"}
		}
		out = append(out, Tool{Name: toolName(c), Desc: c.Summary + " Equivale a: " + c.Usage(), Schema: schema, Confirm: confirm[c.Name]})
	}
	return out
}

func repeats(f cli.Flag) bool { return strings.Contains(f.Desc, "repita") }

func command(tool string) (cli.Cmd, bool) {
	for _, c := range cli.Catalog {
		if offered(c) && toolName(c) == tool {
			return c, true
		}
	}
	return cli.Cmd{}, false
}

// Argv turns a tool call into the arguments of `brpanel`. yes adds --yes to destructive commands:
// only the server passes true, after the user approved; a --yes from the model never gets through.
func Argv(tool string, args json.RawMessage, yes bool) ([]string, error) {
	c, ok := command(tool)
	if !ok {
		return nil, fmt.Errorf("ferramenta desconhecida: %s", tool)
	}
	var in map[string]any
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("os argumentos precisam ser um objeto JSON (veja: %s)", c.Usage())
	}
	flags := map[string]cli.Flag{}
	for _, f := range c.Flags {
		flags[f.Name] = f
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	argv := strings.Fields(c.Name)
	var pos []string
	for _, k := range keys {
		v := in[k]
		if k == "args" {
			pos = texts(v)
			continue
		}
		f, ok := flags[k]
		if !ok {
			return nil, fmt.Errorf("opção desconhecida %q (veja: %s)", k, c.Usage())
		}
		if f.Value == "" {
			if b, _ := v.(bool); b {
				argv = append(argv, "--"+k)
			} else if strings.Contains(f.Desc, "--no-"+k) {
				argv = append(argv, "--no-"+k)
			}
			continue
		}
		for _, s := range texts(v) {
			argv = append(argv, "--"+k+"="+s)
		}
	}
	if yes && c.Danger {
		argv = append(argv, "--yes")
	}
	if len(pos) > 0 { // after "--" so a value that starts with a dash stays an argument
		argv = append(append(argv, "--"), pos...)
	}
	return argv, nil
}

// texts reads a string, a number, a bool or a list of them.
func texts(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return []string{x}
	case float64:
		return []string{strconv.FormatFloat(x, 'f', -1, 64)}
	case bool:
		return []string{strconv.FormatBool(x)}
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, texts(e)...)
		}
		return out
	}
	return []string{fmt.Sprint(v)}
}

// Describe renders a call as the command the user is asked to approve.
func Describe(tool string, args json.RawMessage) string {
	argv, err := Argv(tool, args, false)
	if err != nil {
		return tool
	}
	if i := slices.Index(argv, "--"); i >= 0 && !slices.ContainsFunc(argv[i+1:], func(a string) bool { return strings.HasPrefix(a, "-") }) {
		argv = slices.Delete(argv, i, i+1) // the separator only matters for values that look like options
	}
	for i, a := range argv {
		if strings.ContainsAny(a, " \t'\"$&;|<>()*?!{}") || a == "" {
			argv[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return "brpanel " + strings.Join(argv, " ")
}
