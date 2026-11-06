package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/ai"
	"github.com/viniciusdev772/br_panel/internal/store"
)

// fakeLLM answers the panel's chat requests with canned streams, one per request, and keeps the bodies.
func fakeLLM(t *testing.T, replies ...string) (*httptest.Server, *[]string) {
	t.Helper()
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, replies[len(bodies)-1])
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

func toolCall(id, name, args string) string {
	a, _ := json.Marshal(args)
	return `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"` + id + `","function":{"name":"` + name + `","arguments":` + string(a) + `}}]}}]}` + "\n\ndata: [DONE]\n\n"
}

func textReply(s string) string {
	return `data: {"choices":[{"delta":{"content":"` + s + `"}}]}` + "\n\ndata: [DONE]\n\n"
}

func enableAI(t *testing.T, s *Server, url string) {
	t.Helper()
	set := s.settings()
	set.AIFormat, set.AIBaseURL, set.AIModel, set.AIKey = ai.FormatChat, url, "modelo-x", "chave-secreta-x"
	if err := s.applySettings(set); err != nil {
		t.Fatal(err)
	}
}

// chatEvents posts a conversation and returns the events of the stream.
func chatEvents(t *testing.T, h http.Handler, c *http.Cookie, csrf string, msgs []ai.Msg, approve map[string]bool) (int, []ai.Event) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"messages": msgs, "approve": approve})
	w := do(h, "POST", "/ai/chat", url.Values{"csrf": {csrf}, "payload": {string(payload)}}, c)
	var evs []ai.Event
	dec := json.NewDecoder(w.Body)
	for dec.More() {
		var e ai.Event
		if err := dec.Decode(&e); err != nil {
			break
		}
		evs = append(evs, e)
	}
	return w.Code, evs
}

func TestAIRoutesRequireSessionAndCSRF(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	csrf := csrfOf(t, h, "/ai", c)
	form := func(csrf string) url.Values {
		return url.Values{"csrf": {csrf}, "payload": {`{"messages":[{"role":"user","text":"oi"}]}`}}
	}
	if w := do(h, "GET", "/ai", nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Errorf("GET /ai sem sessão: %d", w.Code)
	}
	if w := do(h, "POST", "/ai/chat", form(csrf)); w.Code != http.StatusSeeOther {
		t.Errorf("POST /ai/chat sem sessão: %d", w.Code)
	}
	if w := do(h, "POST", "/ai/chat", form("errado"), c); w.Code != http.StatusForbidden {
		t.Errorf("POST /ai/chat sem CSRF: %d", w.Code)
	}
	if w := do(h, "POST", "/ai/chat", form(csrf), c); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("IA não configurada deve responder 422: %d", w.Code)
	}
	if body := do(h, "GET", "/ai", nil, c).Body.String(); !strings.Contains(body, "ainda não foi configurado") {
		t.Error("a página deveria pedir a configuração")
	}
	// A CLI token (or the socket) must not be able to spend the API key.
	if code, _, _ := runCLI(t, s, "", "raw", "POST", "/ai/chat"); code == 0 {
		t.Error("/ai/chat não pode atender a CLI")
	}
	if w := do(h, "POST", "/ai/chat", url.Values{"csrf": {csrf}, "payload": {"lixo"}}, c); w.Code != http.StatusBadRequest {
		t.Errorf("conversa inválida: %d", w.Code)
	}
}

func TestAIChatRunsReadOnlyToolsAndKeepsSecrets(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if err := s.store.Create(store.App{Name: "web", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3001, Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	llm, bodies := fakeLLM(t, toolCall("c1", "apps_list", "{}"), textReply("há uma app"))
	enableAI(t, s, llm.URL)
	csrf := csrfOf(t, h, "/ai", c)

	code, evs := chatEvents(t, h, c, csrf, []ai.Msg{{Role: "user", Text: "quais apps?"}}, nil)
	if code != http.StatusOK {
		t.Fatalf("código %d", code)
	}
	kinds := make([]string, len(evs))
	for i, e := range evs {
		kinds[i] = e.T
	}
	if got := strings.Join(kinds, " "); got != "call result text state" {
		t.Fatalf("eventos = %s", got)
	}
	if !strings.Contains(evs[1].Text, "web") {
		t.Errorf("resultado da ferramenta = %q", evs[1].Text)
	}
	if st := evs[3]; len(st.Messages) != 3 || st.Messages[2].Text != "há uma app" {
		t.Errorf("estado = %+v", st.Messages)
	}
	// The second request carries the tool result; the key never appears in a body.
	if len(*bodies) != 2 || !strings.Contains((*bodies)[1], `"role":"tool"`) || !strings.Contains((*bodies)[1], "web") {
		t.Errorf("segundo pedido à IA: %v", *bodies)
	}
	for _, b := range *bodies {
		if strings.Contains(b, "chave-secreta-x") {
			t.Error("a chave da API vazou para o corpo do pedido")
		}
	}
	// The settings page never echoes the key back.
	if body := do(h, "GET", "/settings", nil, c).Body.String(); strings.Contains(body, "chave-secreta-x") {
		t.Error("/settings mostra a chave da API")
	}
}

func TestAIChatAsksBeforeChangingTheServer(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if err := s.store.Create(store.App{Name: "web", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3001, Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	llm, _ := fakeLLM(t,
		toolCall("c1", "apps_env-set", `{"args":["web","A=1"]}`), textReply("feito"),
		toolCall("c2", "apps_env-set", `{"args":["web","B=2"]}`), textReply("tudo bem"))
	enableAI(t, s, llm.URL)
	csrf := csrfOf(t, h, "/ai", c)
	env := func() string { a, _ := s.store.Get("web"); return a.Env }

	// 1. The model asks: the server only reports it.
	_, evs := chatEvents(t, h, c, csrf, []ai.Msg{{Role: "user", Text: "põe A=1"}}, nil)
	last := evs[len(evs)-1]
	var ask ai.Event
	for _, e := range evs {
		if e.T == "confirm" {
			ask = e
		}
	}
	if ask.ID != "c1" || !strings.Contains(ask.Cmd, "brpanel apps env-set") || env() != "" || last.T != "state" {
		t.Fatalf("antes da aprovação nada muda: ask=%+v env=%q", ask, env())
	}

	// 2. The user approves: the command runs (and is logged), then the model answers.
	_, evs = chatEvents(t, h, c, csrf, last.Messages, map[string]bool{"c1": true})
	if env() != "A=1" {
		t.Errorf("aprovado deveria rodar, env = %q (%+v)", env(), evs)
	}
	var logged bool
	for _, e := range s.store.Events(50) {
		logged = logged || strings.Contains(e.Message, "O assistente executou")
	}
	if !logged {
		t.Error("a ação aprovada deveria aparecer nos eventos")
	}

	// 3. Declining runs nothing and tells the model.
	_, evs = chatEvents(t, h, c, csrf, []ai.Msg{{Role: "user", Text: "põe B=2"}}, nil)
	_, evs = chatEvents(t, h, c, csrf, evs[len(evs)-1].Messages, map[string]bool{"c2": false})
	if env() != "A=1" {
		t.Errorf("recusado não pode rodar, env = %q", env())
	}
	if st := evs[len(evs)-1]; st.T != "state" || !strings.HasPrefix(st.Messages[1].Calls[0].Out, "RECUSADO") {
		t.Errorf("a recusa deveria chegar ao modelo: %+v", st)
	}
}

func TestAIChatCannotSmuggleCallsOrYes(t *testing.T) {
	s, h := newTestServer(t)
	c := setup(t, s, h, nil)
	if err := s.store.Create(store.App{Name: "web", Repo: "https://github.com/o/r", Branch: "main", StartCmd: "x", Port: 3001, Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	llm, _ := fakeLLM(t, toolCall("c1", "apps_delete", `{"args":["web"],"yes":true}`), textReply("ok"))
	enableAI(t, s, llm.URL)
	csrf := csrfOf(t, h, "/ai", c)

	// The model cannot add --yes: the option does not exist in the tool, and delete still waits for the user.
	chatEvents(t, h, c, csrf, []ai.Msg{{Role: "user", Text: "apaga"}}, nil)
	if _, ok := s.store.Get("web"); !ok {
		t.Fatal("o modelo apagou a app sem aprovação")
	}
}
