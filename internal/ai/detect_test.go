package ai

import (
	"strings"
	"testing"
)

func TestParseDetected(t *testing.T) {
	text := "Claro! Aqui está:\n```json\n" + `{"kind":"Next.js","build":"npm ci\nnpm run build","start":"next start -p $PORT -H $HOST","needs":["node","npm","rm -rf /"],` +
		`"env_vars":[{"name":"DATABASE_URL","required":true},{"name":"port","required":true},{"name":"PORT","required":true},{"name":"NODE_ENV"}],"notes":["Precisa do Node 20"]}` + "\n```"
	d, err := ParseDetected(text)
	if err != nil {
		t.Fatal(err)
	}
	if d.Build != "npm ci && npm run build" {
		t.Errorf("o comando deveria virar uma linha só: %q", d.Build)
	}
	if len(d.Needs) != 2 || d.Needs[1] != "npm" {
		t.Errorf("programas inválidos deveriam ser descartados: %v", d.Needs)
	}
	if len(d.Env) != 1 || d.Env[0].Name != "DATABASE_URL" {
		t.Errorf("só nomes válidos e que não sejam PORT/HOST/NODE_ENV: %v", d.Env)
	}
	if len(d.Notes) != 1 {
		t.Errorf("start usa $PORT, não deveria ganhar aviso: %v", d.Notes)
	}

	d, err = ParseDetected(`{"kind":"Go","build":"go build -o app .","start":"./app"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Notes) != 1 || !strings.Contains(d.Notes[0], "$PORT") {
		t.Errorf("start sem $PORT deveria avisar: %v", d.Notes)
	}

	for _, bad := range []string{"", "sem json", "{quebrado", "} {"} {
		if _, err := ParseDetected(bad); err == nil {
			t.Errorf("%q deveria falhar", bad)
		}
	}
	long := `{"start":"` + strings.Repeat("x", 900) + `"}`
	if d, _ := ParseDetected(long); len(d.Start) > 500 {
		t.Errorf("comando deveria ser cortado em 500 bytes, veio %d", len(d.Start))
	}
}

func TestParseDetectedToleratesRawLineBreaks(t *testing.T) {
	d, err := ParseDetected("{\"kind\":\"Go\",\n\"build\":\"go build -o app .\ngo vet ./...\",\n\"start\":\"./app\"}")
	if err != nil {
		t.Fatal(err)
	}
	if d.Build != "go build -o app . && go vet ./..." {
		t.Errorf("quebra de linha dentro do texto deveria virar &&: %q", d.Build)
	}
}

func TestSystemPromptPage(t *testing.T) {
	if strings.Contains(SystemPrompt(""), "Tela atual") {
		t.Error("sem tela conhecida não deveria haver a seção")
	}
	if p := SystemPrompt(" O usuário está na página da aplicação \"loja\". "); !strings.HasSuffix(p, "\"loja\".") || !strings.Contains(p, "## Tela atual") {
		t.Errorf("a tela deveria ir ao fim do prompt: %q", p[len(p)-80:])
	}
	// every free tool and every extra the prompt names must be real
	for _, tl := range Tools() {
		if tl.Confirm {
			continue
		}
		if !strings.Contains(systemPrompt, tl.Name) {
			t.Errorf("o prompt não menciona a ferramenta de leitura %s", tl.Name)
		}
	}
}
