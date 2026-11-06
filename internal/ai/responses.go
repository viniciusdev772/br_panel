package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// responses speaks POST /responses. OpenRouter's version is stateless (store and
// previous_response_id are rejected), so the whole history is sent every turn.
type responses struct{ endpoint }

type respItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (c responses) Turn(ctx context.Context, r Request, onText func(string)) (Msg, error) {
	input := []obj{}
	for _, m := range r.Msgs {
		if m.Role != "assistant" {
			input = append(input, obj{"type": "message", "role": "user", "content": []obj{{"type": "input_text", "text": m.Text}}})
			continue
		}
		if m.Text != "" {
			input = append(input, obj{"type": "message", "role": "assistant", "content": []obj{{"type": "output_text", "text": m.Text}}})
		}
		for _, k := range m.Calls {
			input = append(input, obj{"type": "function_call", "call_id": k.ID, "name": k.Name, "arguments": argsText(k.Args)})
		}
		for _, k := range m.Calls {
			input = append(input, obj{"type": "function_call_output", "call_id": k.ID, "output": outText(k)})
		}
	}
	body := obj{"model": c.model, "input": input, "stream": true, "store": false}
	if r.System != "" {
		body["instructions"] = r.System
	}
	if len(r.Tools) > 0 {
		var ts []obj
		for _, t := range r.Tools {
			ts = append(ts, obj{"type": "function", "name": t.Name, "description": t.Desc, "parameters": t.Schema})
		}
		body["tools"] = ts
	}

	var text strings.Builder
	var calls []respItem
	var final []respItem // the closing event repeats the whole output: the fallback if events were missed
	err := c.stream(ctx, "/responses", body, bearer(c.key), func(data string) error {
		var ev struct {
			Type     string          `json:"type"`
			Delta    json.RawMessage `json:"delta"`
			Message  string          `json:"message"`
			Item     *respItem       `json:"item"`
			Response *struct {
				Output []respItem `json:"output"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			return nil
		}
		switch {
		case ev.Type == "error":
			return errors.New("a IA falhou durante a resposta: " + ev.Message)
		case ev.Type == "response.failed" && ev.Response != nil && ev.Response.Error != nil:
			return errors.New("a IA falhou durante a resposta: " + ev.Response.Error.Message)
		case strings.HasSuffix(ev.Type, ".delta") && (strings.HasPrefix(ev.Type, "response.output_text") || strings.HasPrefix(ev.Type, "response.content_part")):
			// OpenAI names it output_text.delta, OpenRouter's beta content_part.delta: both carry a string.
			var s string
			if json.Unmarshal(ev.Delta, &s) == nil && s != "" {
				text.WriteString(s)
				if onText != nil {
					onText(s)
				}
			}
		case ev.Type == "response.output_item.done" && ev.Item != nil && ev.Item.Type == "function_call":
			calls = append(calls, *ev.Item)
		case (ev.Type == "response.completed" || ev.Type == "response.done" || ev.Type == "response.incomplete") && ev.Response != nil:
			final = ev.Response.Output
		}
		return nil
	})
	if len(calls) == 0 {
		for _, it := range final {
			if it.Type == "function_call" {
				calls = append(calls, it)
			}
		}
	}
	if text.Len() == 0 { // no deltas seen: take the finished message instead
		for _, it := range final {
			for _, part := range it.Content {
				if it.Type == "message" && part.Type == "output_text" {
					text.WriteString(part.Text)
					if onText != nil {
						onText(part.Text)
					}
				}
			}
		}
	}
	out := Msg{Role: "assistant", Text: text.String()}
	for i, it := range calls {
		out.Calls = append(out.Calls, Call{ID: callID(it.CallID, i), Name: it.Name, Args: rawArgs(it.Arguments)})
	}
	return out, err
}
