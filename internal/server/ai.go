package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/viniciusdev772/br_panel/internal/ai"
	"github.com/viniciusdev772/br_panel/internal/cli"
)

const (
	aiMaxOutput = 12000 // bytes of a tool result handed to the model
	aiMaxTurns  = 200   // messages accepted back from the browser
)

// handlerRT sends the CLI's requests straight into the panel's CLI-side handler, as the Unix socket would.
type handlerRT struct{ h http.Handler }

func (t handlerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body == nil {
		r.Body = http.NoBody
	}
	w := httptest.NewRecorder()
	t.h.ServeHTTP(w, r)
	return w.Result(), nil
}

// aiPageHint tells the model which screen the user is on, so "este app" and "esse deploy" have a
// meaning. The path comes from the browser, so it is only used to look names up in the store.
func (s *Server) aiPageHint(path string) string {
	path = strings.TrimSpace(strings.SplitN(path, "?", 2)[0])
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case path == "/" || path == "":
		return "O usuário está na tela Servidor (visão geral de aplicações e bancos)."
	case parts[0] == "apps" && len(parts) >= 2 && parts[1] == "new":
		return "O usuário está no formulário de nova aplicação (repositório, comandos, domínio, instâncias)."
	case parts[0] == "apps" && len(parts) >= 2:
		a, ok := s.store.Get(parts[1])
		if !ok {
			return ""
		}
		hint := "O usuário está na página da aplicação \"" + a.Name + "\" (estado do deploy: " + a.Status
		if a.Domain != "" {
			hint += "; domínio: " + a.Domain
		}
		if a.Port != 0 {
			hint += "; porta: " + strconv.Itoa(a.Port)
		}
		hint += ")."
		if len(parts) >= 3 && parts[2] == "edit" {
			hint += " Ele está editando a configuração dela."
		}
		return hint
	case parts[0] == "databases" && len(parts) >= 2 && parts[1] != "new":
		if d, ok := s.store.GetDatabase(parts[1]); ok {
			return "O usuário está na página do banco \"" + d.Name + "\" (" + d.Kind + ", estado: " + d.Status + ")."
		}
	case parts[0] == "settings":
		return "O usuário está em Configurações do painel."
	case parts[0] == "github":
		return "O usuário está na página de integração com o GitHub."
	}
	return ""
}

func (s *Server) handleAIPage(w http.ResponseWriter, r *http.Request) {
	set := s.settings()
	s.render(w, r, "ai", page{Title: "Assistente", Nav: "ai", Set: viewSettings(set), AIReady: set.AIModel != ""})
}

// aiExec runs one tool as the CLI would, inside this process: same routes, same validation.
func (s *Server) aiExec(ctx context.Context, name string, args json.RawMessage, approved bool) string {
	argv, err := ai.Argv(name, args, approved)
	if err != nil {
		return "ERRO: " + err.Error()
	}
	if approved {
		s.store.AddEvent("app", "ia", "O assistente executou, com aprovação: "+ai.Describe(name, args))
	}
	var out bytes.Buffer
	done := make(chan struct{})
	go func() { // cli.RunWith has no context: stop waiting when the request ends
		defer close(done)
		cli.RunWith(handlerRT{s.cliHandler()}, argv, strings.NewReader(""), &out, &out)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return "ERRO: o tempo da conversa acabou antes do comando terminar"
	}
	res := strings.ToValidUTF8(out.String(), "")
	if len(res) > aiMaxOutput { // the end of a log is the part that matters
		res = "…(início cortado)\n" + strings.ToValidUTF8(res[len(res)-aiMaxOutput:], "")
	}
	return res
}

// handleAIChat runs the agent for one request and streams its events as JSON lines. The browser
// keeps the conversation and sends it back, so the server holds no state between calls.
func (s *Server) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if viaCLI(r) { // a CLI token must not be able to spend the panel's API key
		http.NotFound(w, r)
		return
	}
	var in struct {
		Messages []ai.Msg        `json:"messages"`
		Approve  map[string]bool `json:"approve"`
		Page     string          `json:"page"` // path of the screen the user is on, for context
	}
	if json.Unmarshal([]byte(r.PostFormValue("payload")), &in) != nil || len(in.Messages) == 0 || len(in.Messages) > aiMaxTurns {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "conversa inválida"})
		return
	}
	for i := range in.Messages {
		m := &in.Messages[i]
		if m.Role != "assistant" {
			m.Role, m.Calls = "user", nil // only the model's turns may carry tool calls
		}
	}
	set := s.settings()
	p, err := ai.New(set.AIFormat, set.AIBaseURL, set.AIKey, set.AIModel)
	if err != nil {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]string{"error": "Configure a IA em Configurações (modelo e chave)."})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(6 * time.Minute)) // the server-wide 30 s would cut a long answer
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	emit := func(e ai.Event) {
		_ = enc.Encode(e)
		_ = rc.Flush()
	}
	agent := &ai.Agent{P: p, System: ai.SystemPrompt(s.aiPageHint(in.Page)), Tools: append(ai.Tools(), aiExtraTools()...), Exec: s.aiRun, Describe: aiDescribe}
	hist, err := agent.Run(ctx, in.Messages, in.Approve, emit)
	emit(ai.Event{T: "state", Messages: hist})
	if err != nil {
		emit(ai.Event{T: "error", Text: err.Error()})
	}
}
