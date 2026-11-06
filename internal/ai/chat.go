package ai

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// chat speaks POST /chat/completions (OpenAI, OpenRouter, Ollama and most gateways).
type chat struct{ endpoint }

func (c chat) Turn(ctx context.Context, r Request, onText func(string)) (Msg, error) {
	msgs := []obj{}
	if r.System != "" {
		msgs = append(msgs, obj{"role": "system", "content": r.System})
	}
	for _, m := range r.Msgs {
		if m.Role != "assistant" {
			msgs = append(msgs, obj{"role": "user", "content": m.Text})
			continue
		}
		am := obj{"role": "assistant", "content": m.Text}
		var calls []obj
		for _, k := range m.Calls {
			calls = append(calls, obj{"id": k.ID, "type": "function", "function": obj{"name": k.Name, "arguments": argsText(k.Args)}})
		}
		if len(calls) > 0 {
			am["tool_calls"] = calls
			if m.Text == "" {
				am["content"] = nil
			}
		}
		msgs = append(msgs, am)
		for _, k := range m.Calls {
			msgs = append(msgs, obj{"role": "tool", "tool_call_id": k.ID, "content": outText(k)})
		}
	}
	body := obj{"model": c.model, "messages": msgs, "stream": true}
	if len(r.Tools) > 0 {
		var ts []obj
		for _, t := range r.Tools {
			ts = append(ts, obj{"type": "function", "function": obj{"name": t.Name, "description": t.Desc, "parameters": t.Schema}})
		}
		body["tools"] = ts
	}

	type acc struct {
		id, name string
		args     strings.Builder
	}
	accs := map[int]*acc{}
	var text strings.Builder
	err := c.stream(ctx, "/chat/completions", body, bearer(c.key), func(data string) error {
		var ch struct {
			Error   *struct{ Message string } `json:"error"`
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			return nil // a chunk we do not understand is not worth failing the turn
		}
		if ch.Error != nil { // mid-stream failure: the HTTP status was already 200
			return errors.New("a IA falhou durante a resposta: " + ch.Error.Message)
		}
		for _, choice := range ch.Choices {
			if s := choice.Delta.Content; s != "" {
				text.WriteString(s)
				if onText != nil {
					onText(s)
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				a := accs[tc.Index]
				if a == nil {
					a = &acc{}
					accs[tc.Index] = a
				}
				if tc.ID != "" {
					a.id = tc.ID
				}
				if tc.Function.Name != "" {
					a.name = tc.Function.Name
				}
				a.args.WriteString(tc.Function.Arguments)
			}
		}
		return nil
	})
	out := Msg{Role: "assistant", Text: text.String()}
	idx := make([]int, 0, len(accs))
	for i := range accs {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		a := accs[i]
		if a.name != "" {
			out.Calls = append(out.Calls, Call{ID: callID(a.id, i), Name: a.name, Args: rawArgs(a.args.String())})
		}
	}
	return out, err
}
