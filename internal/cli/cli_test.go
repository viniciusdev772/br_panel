package cli

import (
	"bytes"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// words splits a shell-like line with single and double quotes, enough for the catalog examples.
func words(line string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	has := false
	for _, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote, has = r, true
		case r == ' ':
			if cur.Len() > 0 || has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 || has {
		out = append(out, cur.String())
	}
	return out
}

func TestResolve(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		rest     int
	}{
		{"apps list", "apps list", 0},
		{"app ls", "apps list", 0},
		{"apps", "apps list", 0},
		{"ls", "apps list", 0},
		{"APPS Show loja", "apps show", 1},
		{"deploy loja --wait", "apps deploy", 2},
		{"apps env set loja K=V", "apps env-set", 2},
		{"apps env loja", "apps env", 1},
		{"apps env-unset loja K", "apps env-unset", 2},
		{"apps domain add loja d.example.com", "apps domain-add", 2},
		{"apps rm loja --yes", "apps delete", 2},
		{"aplicacoes remove loja", "apps delete", 1},
		{"dbs", "dbs list", 0},
		{"banco ls", "dbs list", 0},
		{"database create --kind redis", "dbs create", 2},
		{"logs loja", "apps logs", 1},
		{"help apps deploy", "help", 2},
		{"status", "status", 0},
	} {
		c, rest := resolve(strings.Fields(tc.in))
		if c == nil || c.Name != tc.want || len(rest) != tc.rest {
			t.Errorf("%q -> %v, rest %v; quero %s com %d resto", tc.in, c, rest, tc.want, tc.rest)
		}
	}
	for _, in := range []string{"appz list", "xyz"} {
		if c, _ := resolve(strings.Fields(in)); c != nil {
			t.Errorf("%q não deveria resolver (%s)", in, c.Name)
		}
	}
}

func TestParse(t *testing.T) {
	c := byName("apps set")
	pos, fl, err := parse(c, []string{"loja", "--memory-mb", "256", "--branch=dev", "-replicas", "2", "--no-auto-deploy", "--json", "--cpu_percent", "50"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pos, []string{"loja"}) || fl["memory-mb"][0] != "256" || fl["branch"][0] != "dev" || fl["replicas"][0] != "2" ||
		fl["auto-deploy"][0] != "false" || fl["json"][0] != "true" || fl["cpu-percent"][0] != "50" {
		t.Errorf("parse: %v %v", pos, fl)
	}
	_, _, err = parse(c, []string{"loja", "--memoria", "1"})
	if err == nil || !strings.Contains(err.Error(), "--memoria") {
		t.Errorf("opção desconhecida deveria falhar: %v", err)
	}
	if _, _, err = parse(c, []string{"loja", "--branch"}); err == nil {
		t.Error("faltou o valor de --branch")
	}
	// A negative number or a value that looks like a flag after -- is positional.
	pos, _, _ = parse(byName("apps env-set"), []string{"loja", "--", "-X=1"})
	if !slices.Equal(pos, []string{"loja", "-X=1"}) {
		t.Errorf("depois de --: %v", pos)
	}
}

func TestEditEnv(t *testing.T) {
	for _, tc := range []struct {
		env   string
		order []string
		set   map[string]string
		unset map[string]bool
		want  string
	}{
		{"A=1\nB=2", []string{"B"}, map[string]string{"B": "9"}, nil, "A=1\nB=9"},
		{"A=1", []string{"C", "D"}, map[string]string{"C": "3", "D": ""}, nil, "A=1\nC=3\nD="},
		{"A=1\nB=2\nC=3", nil, nil, map[string]bool{"B": true}, "A=1\nC=3"},
		{"# nota\nA=1\n\nA=2\r\n", []string{"A"}, map[string]string{"A": "x"}, nil, "# nota\nA=x"},
		{"", []string{"URL"}, map[string]string{"URL": "${{pg.DATABASE_URL}}"}, nil, "URL=${{pg.DATABASE_URL}}"},
	} {
		if got := editEnv(tc.env, tc.order, tc.set, tc.unset); got != tc.want {
			t.Errorf("editEnv(%q) = %q; quero %q", tc.env, got, tc.want)
		}
	}
}

// Every command has a handler, and every example in the catalog is a line the CLI accepts: the
// guide must never teach a command that does not parse.
func TestCatalogConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Catalog {
		if handlers[c.Name] == nil {
			t.Errorf("%s sem handler", c.Name)
		}
		if seen[c.Name] {
			t.Errorf("%s repetido", c.Name)
		}
		seen[c.Name] = true
		if len(c.Examples) == 0 || c.Summary == "" || c.Group == "" {
			t.Errorf("%s precisa de grupo, resumo e exemplo", c.Name)
		}
		for _, e := range c.Examples {
			line := e
			if _, after, ok := strings.Cut(e, "| "); ok { // printf '%s' x | brpanel ...
				line = after
			}
			w := words(line)
			if len(w) == 0 || w[0] != "brpanel" {
				t.Errorf("exemplo de %s não começa com brpanel: %q", c.Name, e)
				continue
			}
			got, rest := resolve(w[1:])
			if got == nil || got.Name != c.Name {
				t.Errorf("exemplo %q resolve para %v, não para %s", e, got, c.Name)
				continue
			}
			pos, _, err := parse(got, rest)
			if err != nil {
				t.Errorf("exemplo %q não passa no parser: %v", e, err)
			} else if len(pos) < got.Min && got.Name != "apps create" {
				t.Errorf("exemplo %q tem poucos argumentos (%d < %d)", e, len(pos), got.Min)
			}
		}
	}
	for n := range handlers {
		if !seen[n] {
			t.Errorf("handler %s não está no catálogo", n)
		}
	}
	g := Guide()
	for _, c := range Catalog {
		if !strings.Contains(g, "\n"+c.Name+"\n") {
			t.Errorf("o guia não descreve %s", c.Name)
		}
	}
	if !strings.Contains(HelpOverview(), "apps deploy") || !strings.Contains(HelpCommand(*byName("apps delete")), "--yes") {
		t.Error("help incompleto")
	}
}

type refuse struct{ t *testing.T }

func (r refuse) RoundTrip(req *http.Request) (*http.Response, error) {
	r.t.Errorf("nenhuma requisição era esperada, mas veio %s %s", req.Method, req.URL.Path)
	return nil, http.ErrNotSupported
}

func runCLI(t *testing.T, args ...string) (code int, out, errs string) {
	t.Helper()
	var o, e bytes.Buffer
	code = RunWith(refuse{t}, args, strings.NewReader(""), &o, &e)
	return code, o.String(), e.String()
}

// Mistakes are caught before the panel is contacted, with the next step spelled out.
func TestUsageErrors(t *testing.T) {
	code, _, errs := runCLI(t, "apps", "delete", "loja")
	if code != 2 || !strings.Contains(errs, "--yes") || !strings.HasPrefix(errs, "ERRO:") {
		t.Errorf("apagar sem --yes: %d %q", code, errs)
	}
	code, _, errs = runCLI(t, "apps", "deploy")
	if code != 2 || !strings.Contains(errs, "USO: brpanel apps deploy NOME") || !strings.Contains(errs, "EXEMPLO:") {
		t.Errorf("faltou NOME: %d %q", code, errs)
	}
	code, _, errs = runCLI(t, "appz", "lst")
	if code != 2 || !strings.Contains(errs, "DICA: você quis dizer: brpanel") {
		t.Errorf("comando errado: %d %q", code, errs)
	}
	code, _, errs = runCLI(t, "apps", "list", "--formato", "x")
	if code != 2 || !strings.Contains(errs, "--json") {
		t.Errorf("opção errada deve listar as válidas: %d %q", code, errs)
	}
	code, _, errs = runCLI(t, "apps", "frobnicate", "loja")
	if code != 2 || !strings.Contains(errs, "não recebe argumentos") {
		t.Errorf("subcomando inventado: %d %q", code, errs)
	}
	code, _, errs = runCLI(t, "apps", "auth-add", "loja", "u")
	if code != 2 || !strings.Contains(errs, "--password-stdin") {
		t.Errorf("senha na linha de comando: %d %q", code, errs)
	}
	code, out, _ := runCLI(t, "help", "apps", "deploy")
	if code != 0 || !strings.Contains(out, "--wait") {
		t.Errorf("help de comando: %d %q", code, out)
	}
	code, out, _ = runCLI(t, "guide")
	if code != 0 || !strings.Contains(out, "REGRAS DE OURO") {
		t.Errorf("guide: %d", code)
	}
}
