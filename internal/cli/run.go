package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DefaultData is where the panel keeps its data (and its CLI socket) unless -data says otherwise.
const DefaultData = "/var/lib/brpanel"

// SocketName is the file, inside the data directory, where the panel listens for the CLI.
const SocketName = "brpanel.sock"

// IsCommand reports whether the first argument means "run the CLI" instead of "start the server".
// The server takes only flags, so any word is a command (unknown ones get a helpful error).
func IsCommand(args []string) bool { return len(args) > 0 && !strings.HasPrefix(args[0], "-") }

// Run executes one command (args without the program name) against the panel and returns the exit
// code: 0 success, 1 the panel refused or failed, 2 the command line itself is wrong.
func Run(args []string, in io.Reader, out, errw io.Writer) int { return run(nil, args, in, out, errw) }

// RunWith is Run over a custom transport instead of the Unix socket (used by tests).
func RunWith(rt http.RoundTripper, args []string, in io.Reader, out, errw io.Writer) int {
	return run(rt, args, in, out, errw)
}

// cliErr is a failure with the extra lines (DICA/USO/EXEMPLO) that tell the agent what to do next.
type cliErr struct {
	msg   string
	extra []string
	code  int
}

func (e *cliErr) Error() string { return e.msg }

func fail(msg string, extra ...string) error { return &cliErr{msg: msg, extra: extra, code: 1} }

func usageErr(c *Cmd, msg string) error {
	e := &cliErr{msg: msg, code: 2, extra: []string{"USO: " + c.Usage()}}
	if len(c.Examples) > 0 {
		e.extra = append(e.extra, "EXEMPLO: "+c.Examples[0])
	}
	return e
}

// call carries one parsed command to its handler.
type call struct {
	c    *client
	rt   http.RoundTripper // set only by RunWith; login uses it instead of the network
	cmd  *Cmd
	pos  []string
	fl   map[string][]string
	in   io.Reader
	out  io.Writer
	json bool
}

func (x *call) set(n string) bool { _, ok := x.fl[n]; return ok }
func (x *call) s(n string) string {
	v := x.fl[n]
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}
func (x *call) list(n string) []string { return x.fl[n] }
func (x *call) on(n string) bool {
	v := x.s(n)
	return x.set(n) && v != "false" && v != "off" && v != "0"
}

// num reads an integer flag; ok is false when the flag was not given.
func (x *call) num(n string) (v int, ok bool, err error) {
	if !x.set(n) {
		return 0, false, nil
	}
	v, err = strconv.Atoi(strings.TrimSpace(x.s(n)))
	if err != nil {
		return 0, true, usageErr(x.cmd, fmt.Sprintf("--%s precisa de um número inteiro, recebi %q", n, x.s(n)))
	}
	return v, true, nil
}

// ok prints the success line (or JSON) of an action.
func (x *call) ok(msg string) {
	if x.json {
		writeJSON(x.out, map[string]any{"ok": true, "message": msg})
		return
	}
	fmt.Fprintln(x.out, "OK: "+msg)
}

func run(rt http.RoundTripper, args []string, in io.Reader, out, errw io.Writer) int {
	cmd, rest := resolve(args)
	if cmd == nil {
		return report(errw, unknownCommand(args), false)
	}
	pos, fl, err := parse(cmd, rest)
	if err != nil {
		return report(errw, err, false)
	}
	asJSON := len(fl["json"]) > 0 && fl["json"][len(fl["json"])-1] != "false"
	if cmd.Args == "" && len(pos) > 0 { // "apps frobnicate loja" must not quietly run "apps list"
		e := usageErr(cmd, fmt.Sprintf("este comando não recebe argumentos, mas veio: %s", strings.Join(pos, " ")))
		e.(*cliErr).extra = append(e.(*cliErr).extra, "DICA: veja os comandos existentes com: brpanel help")
		return report(errw, e, asJSON)
	}
	if len(pos) < cmd.Min {
		return report(errw, usageErr(cmd, "faltam argumentos"), asJSON)
	}
	if cmd.Danger && !(len(fl["yes"]) > 0 && fl["yes"][0] != "false") {
		return report(errw, &cliErr{msg: "este comando apaga dados e não pode ser desfeito", code: 2,
			extra: []string{"DICA: se tem certeza, repita o mesmo comando com --yes no final", "USO: " + cmd.Usage()}}, asJSON)
	}
	x := &call{cmd: cmd, pos: pos, fl: fl, in: in, out: out, json: asJSON, rt: rt}
	if !offline[cmd.Name] {
		base, remote := "http://brpanel", false
		if rt == nil {
			if cfg, ok := loadConfig(); ok && !x.on("local") { // logged in: use the remote server
				rt, base, remote = bearerRT{cfg.Token, http.DefaultTransport}, cfg.Server, true
			} else if runtime.GOOS != "linux" && !x.set("data") { // the panel's socket exists only on the Linux server
				return report(errw, fail("você ainda não entrou em nenhum servidor",
					"DICA: crie um token na aba CLI do painel e rode: brpanel login ENDEREÇO-DO-PAINEL",
					"DICA: o guia completo é: brpanel guide"), asJSON)
			} else {
				data := DefaultData
				if v := fl["data"]; len(v) > 0 {
					data = v[len(v)-1]
				}
				rt = unixTransport(filepath.Join(data, SocketName))
			}
		}
		x.c = newClient(rt, base, remote)
	}
	if err := handlers[cmd.Name](x); err != nil {
		return report(errw, err, asJSON)
	}
	notice(x, errw)
	return 0
}

// report prints a failure the way an agent can act on: ERRO first, then the next steps.
func report(w io.Writer, err error, asJSON bool) int {
	ce := &cliErr{}
	if !errors.As(err, &ce) {
		ce = &cliErr{msg: err.Error(), code: 1}
	}
	if asJSON {
		writeJSON(w, map[string]any{"ok": false, "error": ce.msg, "hints": ce.extra})
		return ce.code
	}
	fmt.Fprintln(w, "ERRO: "+ce.msg)
	for _, l := range ce.extra {
		fmt.Fprintln(w, l)
	}
	return ce.code
}

// offline commands never contact a server (login builds its own client).
var offline = map[string]bool{"help": true, "guide": true, "skills install": true, "version": true, "update": true, "login": true, "logout": true, "whoami": true}

func newClient(rt http.RoundTripper, base string, remote bool) *client {
	return &client{base: base, remote: remote, hc: &http.Client{Transport: rt, Timeout: 2 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func unixTransport(sock string) http.RoundTripper {
	return &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, "unix", sock)
		switch {
		case err == nil:
			return c, nil
		case errors.Is(err, os.ErrPermission):
			return nil, fail("sem permissão para falar com o painel ("+sock+")", "DICA: rode como root, por exemplo: sudo brpanel ...")
		case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ECONNREFUSED):
			return nil, fail("não consegui falar com o painel ("+sock+")",
				"DICA: o painel precisa estar rodando neste servidor. Confira com: systemctl status brpanel",
				"DICA: se a pasta de dados não é "+DefaultData+", passe --data PASTA",
				"DICA: para controlar um servidor remoto (de outro computador), use: brpanel login ENDEREÇO")
		}
		return nil, err
	}}
}

// parse splits args into positional arguments and flags, accepting --f v, --f=v, -f v and --no-f.
func parse(c *Cmd, args []string) (pos []string, fl map[string][]string, err error) {
	valid := map[string]Flag{}
	for _, f := range append(append([]Flag{}, c.Flags...), globalFlags...) {
		valid[f.Name] = f
	}
	fl = map[string][]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' || (a[1] >= '0' && a[1] <= '9') {
			pos = append(pos, a)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		name = strings.ReplaceAll(strings.ToLower(name), "_", "-")
		negate := false
		if _, known := valid[name]; !known && strings.HasPrefix(name, "no-") {
			if base, ok := valid[name[3:]]; ok && base.Value == "" { // --no-auto-deploy
				name, negate = name[3:], true
			}
		}
		f, ok := valid[name]
		if !ok {
			return nil, nil, &cliErr{code: 2, msg: fmt.Sprintf("opção desconhecida: --%s", name), extra: []string{
				"DICA: as opções deste comando são: " + flagNames(c), "USO: " + c.Usage()}}
		}
		switch {
		case f.Value == "": // switch
			v := "true"
			if hasVal {
				v = strings.ToLower(val)
			}
			if negate {
				v = "false"
			}
			fl[name] = append(fl[name], v)
		case hasVal:
			fl[name] = append(fl[name], val)
		case i+1 < len(args):
			i++
			fl[name] = append(fl[name], args[i])
		default:
			return nil, nil, usageErr(c, fmt.Sprintf("faltou o valor de --%s (%s)", name, f.Value))
		}
	}
	return pos, fl, nil
}

func flagNames(c *Cmd) string {
	var n []string
	for _, f := range append(append([]Flag{}, c.Flags...), globalFlags...) {
		n = append(n, "--"+f.Name)
	}
	return strings.Join(n, " ")
}

// unknownCommand names the closest real command so a confused agent can fix the line in one step.
func unknownCommand(args []string) error {
	typed := strings.ToLower(strings.Join(args[:min(len(args), 2)], " "))
	best, bestD := "", 1<<30
	for _, c := range Catalog {
		for _, n := range append([]string{c.Name}, c.Aliases...) {
			if d := distance(typed, n); d < bestD {
				best, bestD = n, d
			}
		}
	}
	e := &cliErr{code: 2, msg: fmt.Sprintf("comando desconhecido: %q", strings.Join(args, " "))}
	if bestD <= max(3, len(typed)/3) {
		e.extra = append(e.extra, fmt.Sprintf("DICA: você quis dizer: brpanel %s", best))
	}
	e.extra = append(e.extra, "DICA: veja todos os comandos com: brpanel help", "DICA: as palavras dos comandos são em inglês (apps, dbs, deploy, logs); os textos são em português")
	return e
}

// distance is the Levenshtein distance between a and b.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// sortedKeys returns the keys of m in order, for stable output.
func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for n := range m {
		k = append(k, n)
	}
	sort.Strings(k)
	return k
}
