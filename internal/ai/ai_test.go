package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/viniciusdev772/br_panel/internal/cli"
)

func TestEveryCommandIsClassified(t *testing.T) {
	for _, c := range cli.Catalog {
		if !Classified(c.Name) {
			t.Errorf("comando %q do catálogo não foi classificado em ai/tools.go (livre, com confirmação ou retido)", c.Name)
		}
	}
	for _, tl := range Tools() {
		if strings.ContainsAny(tl.Name, " ") || len(tl.Name) > 64 {
			t.Errorf("nome de ferramenta inválido: %q", tl.Name)
		}
	}
}

func TestArgv(t *testing.T) {
	got, err := Argv("apps_set", json.RawMessage(`{"args":["loja"],"replicas":2,"memory-mb":"512","auto-deploy":false,"yes":true}`), false)
	if err == nil {
		t.Fatalf("--yes do modelo deveria ser recusado, veio %v", got)
	}
	got, err = Argv("apps_set", json.RawMessage(`{"args":["-loja"],"replicas":2,"auto-deploy":false}`), false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"apps", "set", "--no-auto-deploy", "--replicas=2", "--", "-loja"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %v, quero %v", got, want)
	}
	got, _ = Argv("apps_delete", json.RawMessage(`{"args":["loja"]}`), true)
	if !reflect.DeepEqual(got, []string{"apps", "delete", "--yes", "--", "loja"}) {
		t.Errorf("delete aprovado = %v", got)
	}
	if _, err := Argv("raw", nil, false); err == nil {
		t.Error("raw não deve ser oferecido ao modelo")
	}
	if d := Describe("apps_env-set", json.RawMessage(`{"args":["loja","A=a b"]}`)); d != "brpanel apps env-set -- loja 'A=a b'" {
		t.Errorf("describe = %q", d)
	}
}

// fake serves canned SSE bodies, one per request, and records the requests.
func fake(t *testing.T, bodies ...string) (*httptest.Server, *[]string) {
	t.Helper()
	var reqs []string
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		reqs = append(reqs, r.URL.Path+" "+r.Header.Get("Authorization")+"|"+r.Header.Get("x-api-key")+" "+string(b))
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, bodies[n])
		n++
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

var tools = []Tool{{Name: "apps_list", Desc: "d", Schema: obj{"type": "object"}}}

// roundTrip drives one tool call and its answer through a provider.
func roundTrip(t *testing.T, format string, first, second string) (Msg, Msg, []string) {
	t.Helper()
	srv, reqs := fake(t, first, second)
	p, err := New(format, srv.URL, "k3y", "m")
	if err != nil {
		t.Fatal(err)
	}
	var chunks []string
	m1, err := p.Turn(context.Background(), Request{System: "sys", Msgs: []Msg{{Role: "user", Text: "oi"}}, Tools: tools}, func(s string) { chunks = append(chunks, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(m1.Calls) != 1 || m1.Calls[0].Name != "apps_list" || m1.Calls[0].ID != "c1" || string(m1.Calls[0].Args) != `{"a":1}` {
		t.Fatalf("%s: chamada = %+v", format, m1)
	}
	m1.Calls[0].Out, m1.Calls[0].Done = "web ok", true
	m2, err := p.Turn(context.Background(), Request{System: "sys", Msgs: []Msg{{Role: "user", Text: "oi"}, m1}, Tools: tools}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Text != "pronto" || len(m2.Calls) != 0 {
		t.Fatalf("%s: resposta final = %+v", format, m2)
	}
	return m1, m2, *reqs
}

func TestChat(t *testing.T) {
	m1, _, reqs := roundTrip(t, FormatChat,
		": OPENROUTER PROCESSING\n\n"+
			`data: {"choices":[{"delta":{"content":"vou "}}]}`+"\n\n"+
			`data: {"choices":[{"delta":{"content":"ver"}}]}`+"\n\n"+
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"apps_list","arguments":"{\"a\""}}]}}]}`+"\n\n"+
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":1}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n"+"data: [DONE]\n\n",
		`data: {"choices":[{"delta":{"content":"pronto"}}]}`+"\n\ndata: [DONE]\n\n")
	if m1.Text != "vou ver" {
		t.Errorf("texto = %q", m1.Text)
	}
	if !strings.HasPrefix(reqs[1], "/chat/completions Bearer k3y|") || !strings.Contains(reqs[1], `"role":"tool","tool_call_id":"c1"`) || !strings.Contains(reqs[1], `web ok`) {
		t.Errorf("pedido 2 sem o resultado da ferramenta: %s", reqs[1])
	}
}

func TestResponses(t *testing.T) {
	_, _, reqs := roundTrip(t, FormatResponses,
		`data: {"type":"response.created"}`+"\n\n"+
			`data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"c1","name":"apps_list","arguments":"{\"a\":1}"}}`+"\n\n"+
			`data: {"type":"response.done","response":{"output":[]}}`+"\n\n",
		`data: {"type":"response.output_text.delta","delta":"pro"}`+"\n\n"+`data: {"type":"response.content_part.delta","delta":"nto"}`+"\n\n")
	if !strings.HasPrefix(reqs[1], "/responses ") || !strings.Contains(reqs[1], `"type":"function_call_output"`) || !strings.Contains(reqs[1], `"call_id":"c1"`) || !strings.Contains(reqs[1], `"store":false`) {
		t.Errorf("pedido 2: %s", reqs[1])
	}
}

func TestResponsesFallsBackToTheFinalOutput(t *testing.T) {
	srv, _ := fake(t, `data: {"type":"response.completed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"só no fim"}]}]}}`+"\n\n")
	p, _ := New(FormatResponses, srv.URL, "", "m")
	m, err := p.Turn(context.Background(), Request{Msgs: []Msg{{Role: "user", Text: "x"}}}, nil)
	if err != nil || m.Text != "só no fim" {
		t.Errorf("%+v %v", m, err)
	}
}

func TestMessages(t *testing.T) {
	_, _, reqs := roundTrip(t, FormatMessages,
		"event: message_start\n"+`data: {"type":"message_start"}`+"\n\n"+
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"c1","name":"apps_list"}}`+"\n\n"+
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`+"\n\n"+
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"a\":"}}`+"\n\n"+
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"1}"}}`+"\n\n"+
			`data: {"type":"ping"}`+"\n\n"+`data: {"type":"message_stop"}`+"\n\n",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`+"\n\n"+
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pronto"}}`+"\n\n")
	if !strings.HasPrefix(reqs[1], "/messages Bearer k3y|k3y ") || !strings.Contains(reqs[1], `"tool_use_id":"c1","type":"tool_result"`) ||
		!strings.Contains(reqs[1], `"system":"sys"`) || !strings.Contains(reqs[1], `"input_schema"`) {
		t.Errorf("pedido 2: %s", reqs[1])
	}
}

func TestErrors(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(402)
		fmt.Fprint(w, `{"error":{"code":402,"message":"créditos insuficientes"}}`)
	}))
	defer bad.Close()
	for _, f := range Formats {
		p, _ := New(f, bad.URL, "k", "m")
		if _, err := p.Turn(context.Background(), Request{Msgs: []Msg{{Role: "user", Text: "x"}}}, nil); err == nil || !strings.Contains(err.Error(), "402") || !strings.Contains(err.Error(), "créditos") {
			t.Errorf("%s: erro = %v", f, err)
		}
	}
	srv, _ := fake(t, `data: {"error":{"message":"caiu"},"choices":[{"delta":{"content":""},"finish_reason":"error"}]}`+"\n\n")
	p, _ := New(FormatChat, srv.URL, "k", "m")
	if _, err := p.Turn(context.Background(), Request{Msgs: []Msg{{Role: "user", Text: "x"}}}, nil); err == nil || !strings.Contains(err.Error(), "caiu") {
		t.Errorf("erro no meio do stream = %v", err)
	}
	if _, err := New("xml", "https://a", "", "m"); err == nil {
		t.Error("formato inválido aceito")
	}
	for _, u := range []string{"http://openrouter.ai/api/v1", "ftp://x", "https://u:p@x.com", "https://x.com/?a=1", ""} {
		if ValidBaseURL(u) == nil {
			t.Errorf("%q deveria ser recusado", u)
		}
	}
	for _, u := range []string{"https://openrouter.ai/api/v1", "http://localhost:11434/v1", "http://127.0.0.1:8080"} {
		if err := ValidBaseURL(u); err != nil {
			t.Errorf("%q: %v", u, err)
		}
	}
}

// scripted answers with the turns it was given, one per call.
type scripted struct {
	turns []Msg
	seen  [][]Msg
}

func (s *scripted) Turn(_ context.Context, r Request, _ func(string)) (Msg, error) {
	s.seen = append(s.seen, append([]Msg(nil), r.Msgs...))
	m := s.turns[0]
	s.turns = s.turns[1:]
	return m, nil
}

func TestAgentConfirmsBeforeChangingAnything(t *testing.T) {
	p := &scripted{turns: []Msg{
		{Role: "assistant", Calls: []Call{{ID: "a", Name: "read", Args: json.RawMessage(`{}`)}, {ID: "b", Name: "write", Args: json.RawMessage(`{}`)}}},
		{Role: "assistant", Text: "feito"},
	}}
	var ran []string
	a := &Agent{P: p, Tools: []Tool{{Name: "read"}, {Name: "write", Confirm: true}},
		Exec: func(_ context.Context, name string, _ json.RawMessage, approved bool) string {
			ran = append(ran, fmt.Sprintf("%s:%v", name, approved))
			return "ok " + name
		}}
	var events []string
	emit := func(e Event) { events = append(events, e.T+":"+e.Name) }

	hist, err := a.Run(context.Background(), []Msg{{Role: "user", Text: "faz"}}, nil, emit)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ran, []string{"read:false"}) {
		t.Fatalf("antes da resposta do usuário só a leitura roda: %v", ran)
	}
	if got := strings.Join(events, " "); got != "call:read call:write result:read confirm:write" {
		t.Errorf("eventos = %s", got)
	}

	hist, err = a.Run(context.Background(), hist, map[string]bool{"b": true}, emit)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ran, []string{"read:false", "write:true"}) || hist[len(hist)-1].Text != "feito" {
		t.Errorf("ran = %v, hist = %+v", ran, hist)
	}

	// Recusar: o modelo recebe a recusa e nada roda.
	p.turns = []Msg{{Role: "assistant", Calls: []Call{{ID: "c", Name: "write", Args: json.RawMessage(`{}`)}}}, {Role: "assistant", Text: "ok, não fiz"}}
	ran = nil
	hist, _ = a.Run(context.Background(), []Msg{{Role: "user", Text: "x"}}, nil, emit)
	hist, _ = a.Run(context.Background(), hist, map[string]bool{"c": false}, emit)
	if len(ran) != 0 || !strings.HasPrefix(hist[1].Calls[0].Out, "RECUSADO") {
		t.Errorf("recusa: ran = %v, out = %q", ran, hist[1].Calls[0].Out)
	}
}

func TestAgentStopsAtTheStepLimit(t *testing.T) {
	loop := Msg{Role: "assistant", Calls: []Call{{ID: "x", Name: "read", Args: json.RawMessage(`{}`)}}}
	p := &scripted{}
	for i := 0; i < MaxSteps+2; i++ {
		l := loop
		l.Calls = []Call{{ID: fmt.Sprint(i), Name: "read", Args: json.RawMessage(`{}`)}}
		p.turns = append(p.turns, l)
	}
	a := &Agent{P: p, Tools: []Tool{{Name: "read"}}, Exec: func(context.Context, string, json.RawMessage, bool) string { return "ok" }}
	if _, err := a.Run(context.Background(), []Msg{{Role: "user", Text: "x"}}, nil, func(Event) {}); err == nil || !strings.Contains(err.Error(), "limite") {
		t.Errorf("erro = %v", err)
	}
}
