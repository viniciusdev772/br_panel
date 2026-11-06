package ai

import (
	"context"
	"encoding/json"
	"errors"
)

// MaxSteps bounds the model turns of one request, so a confused model cannot loop forever.
const MaxSteps = 10

// Event is one line of the stream the browser reads.
type Event struct {
	T        string          `json:"t"` // text | call | result | confirm | state | error
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name,omitempty"`
	Text     string          `json:"text,omitempty"` // text chunk, tool output, or error message
	Cmd      string          `json:"cmd,omitempty"`  // the command line a confirmation is about
	Args     json.RawMessage `json:"args,omitempty"`
	Messages []Msg           `json:"messages,omitempty"`
}

// Agent runs the loop: ask the model, run the tools it called, answer with the results.
type Agent struct {
	P      Provider
	System string
	Tools  []Tool
	// Exec runs a tool. approved is true when the user said yes to a Confirm tool.
	Exec func(ctx context.Context, name string, args json.RawMessage, approved bool) string
	// Describe renders a call as the command line the user is asked to approve.
	Describe func(name string, args json.RawMessage) string
}

func (a *Agent) tool(name string) (Tool, bool) {
	for _, t := range a.Tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// Run continues the conversation in hist, which ends with a user message or with an assistant
// message whose tool calls are waiting for approval (approve maps call id to the user's answer).
// It returns the extended history; when it stops with calls still unanswered, a "confirm" event
// was emitted for each and the caller must come back with the user's decision.
func (a *Agent) Run(ctx context.Context, hist []Msg, approve map[string]bool, emit func(Event)) ([]Msg, error) {
	for step := 0; ; step++ {
		if len(hist) == 0 {
			return hist, errors.New("conversa vazia")
		}
		if last := &hist[len(hist)-1]; last.Role == "assistant" {
			if len(last.Calls) == 0 {
				return hist, nil
			}
			if a.resolve(ctx, last, approve, emit) {
				return hist, nil
			}
			approve = nil // the decisions belonged to the calls that existed when the user answered
		}
		if step >= MaxSteps {
			return hist, errors.New("o assistente passou do limite de passos; peça de novo com algo mais específico")
		}
		m, err := a.P.Turn(ctx, Request{System: a.System, Msgs: hist, Tools: a.Tools}, func(s string) { emit(Event{T: "text", Text: s}) })
		if err != nil {
			return hist, err
		}
		if m.Text == "" && len(m.Calls) == 0 {
			return hist, errors.New("a IA respondeu vazio")
		}
		hist = append(hist, m)
		for _, c := range m.Calls {
			emit(Event{T: "call", ID: c.ID, Name: c.Name, Cmd: a.describe(c), Args: c.Args})
		}
	}
}

func (a *Agent) describe(c Call) string {
	if a.Describe == nil {
		return c.Name
	}
	return a.Describe(c.Name, c.Args)
}

// resolve answers the open calls of m: read-only tools run at once, the others only with the
// user's approval. It reports whether some call is still waiting for it.
func (a *Agent) resolve(ctx context.Context, m *Msg, approve map[string]bool, emit func(Event)) (waiting bool) {
	for i := range m.Calls {
		c := &m.Calls[i]
		if c.Done {
			continue
		}
		t, ok := a.tool(c.Name)
		yes, decided := approve[c.ID]
		switch {
		case !ok:
			c.Out = "ERRO: ferramenta desconhecida: " + c.Name
		case t.Confirm && !decided:
			waiting = true
			emit(Event{T: "confirm", ID: c.ID, Name: c.Name, Cmd: a.describe(*c), Args: c.Args})
			continue
		case t.Confirm && !yes:
			c.Out = "RECUSADO: o usuário não autorizou esta ação. Não tente de novo sem ele pedir."
		default:
			c.Out = a.Exec(ctx, c.Name, c.Args, t.Confirm)
		}
		c.Done = true
		emit(Event{T: "result", ID: c.ID, Name: c.Name, Text: c.Out})
	}
	return waiting
}
