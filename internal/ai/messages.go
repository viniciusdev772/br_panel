package ai

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// messages speaks POST /messages (Anthropic's format, also served by OpenRouter).
type messages struct{ endpoint }

// maxTokens is required by the native API and harmless where it is optional.
const maxTokens = 8192

func (c messages) Turn(ctx context.Context, r Request, onText func(string)) (Msg, error) {
	msgs := []obj{}
	for _, m := range r.Msgs {
		if m.Role != "assistant" {
			msgs = append(msgs, obj{"role": "user", "content": m.Text})
			continue
		}
		var blocks []obj
		if m.Text != "" {
			blocks = append(blocks, obj{"type": "text", "text": m.Text})
		}
		var results []obj
		for _, k := range m.Calls {
			blocks = append(blocks, obj{"type": "tool_use", "id": k.ID, "name": k.Name, "input": argsObject(k.Args)})
			results = append(results, obj{"type": "tool_result", "tool_use_id": k.ID, "content": outText(k), "is_error": strings.HasPrefix(k.Out, "ERRO")})
		}
		if len(blocks) == 0 {
			continue
		}
		msgs = append(msgs, obj{"role": "assistant", "content": blocks})
		if len(results) > 0 {
			msgs = append(msgs, obj{"role": "user", "content": results})
		}
	}
	body := obj{"model": c.model, "max_tokens": maxTokens, "messages": msgs, "stream": true}
	if r.System != "" {
		body["system"] = r.System
	}
	if len(r.Tools) > 0 {
		var ts []obj
		for _, t := range r.Tools {
			ts = append(ts, obj{"name": t.Name, "description": t.Desc, "input_schema": t.Schema})
		}
		body["tools"] = ts
	}
	// OpenRouter takes a bearer token, Anthropic x-api-key: sending both reaches either one.
	hdr := bearer(c.key)
	if c.key != "" {
		hdr["x-api-key"] = c.key
	}
	if hdr == nil {
		hdr = map[string]string{}
	}
	hdr["anthropic-version"] = "2023-06-01"

	type block struct {
		typ, id, name string
		text, json    strings.Builder
	}
	blocks := map[int]*block{}
	err := c.stream(ctx, "/messages", body, hdr, func(data string) error {
		var ev struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			return nil // ping and anything newer than this code
		}
		switch ev.Type {
		case "error":
			return errors.New("a IA falhou durante a resposta: " + ev.Error.Message)
		case "content_block_start":
			blocks[ev.Index] = &block{typ: ev.ContentBlock.Type, id: ev.ContentBlock.ID, name: ev.ContentBlock.Name}
		case "content_block_delta":
			b := blocks[ev.Index]
			if b == nil {
				return nil
			}
			switch ev.Delta.Type {
			case "text_delta":
				b.text.WriteString(ev.Delta.Text)
				if onText != nil {
					onText(ev.Delta.Text)
				}
			case "input_json_delta": // fragments are not valid JSON until the block ends
				b.json.WriteString(ev.Delta.PartialJSON)
			}
		}
		return nil
	})
	out := Msg{Role: "assistant"}
	idx := make([]int, 0, len(blocks))
	for i := range blocks {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		b := blocks[i]
		switch b.typ {
		case "text":
			out.Text += b.text.String()
		case "tool_use":
			out.Calls = append(out.Calls, Call{ID: callID(b.id, i), Name: b.name, Args: rawArgs(b.json.String())})
		}
	}
	return out, err
}
