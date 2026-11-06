// Package ai is the panel's assistant: a tool-calling loop that speaks the three wire formats an
// OpenAI-compatible endpoint (OpenRouter, OpenAI, Ollama, Anthropic…) may offer: Chat Completions,
// Responses and Messages. It depends only on the standard library and the CLI catalog, whose
// commands are the tools: the model can do exactly what `brpanel` can, through the same validation.
package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The wire formats, as stored in the settings.
const (
	FormatChat      = "chat"
	FormatResponses = "responses"
	FormatMessages  = "messages"
)

// Formats lists the valid values of the format setting.
var Formats = []string{FormatChat, FormatResponses, FormatMessages}

// Call is one tool call of the model. Out holds the result once Done.
type Call struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
	Out  string          `json:"out,omitempty"`
	Done bool            `json:"done,omitempty"`
}

// Msg is one turn of the conversation, independent of the wire format: the three adapters turn it
// into their own shape. Results of tool calls travel inside the assistant turn that asked for them.
type Msg struct {
	Role  string `json:"role"` // user | assistant
	Text  string `json:"text,omitempty"`
	Calls []Call `json:"calls,omitempty"`
}

// Tool is a function the model may call.
type Tool struct {
	Name, Desc string
	Schema     map[string]any // JSON Schema of the arguments
	Confirm    bool           // changes the server: needs the user's approval first
}

// Request is everything one model turn needs.
type Request struct {
	System string
	Msgs   []Msg
	Tools  []Tool
}

// Provider asks the model for its next turn, streaming the text as it arrives.
type Provider interface {
	Turn(ctx context.Context, r Request, onText func(string)) (Msg, error)
}

// New builds the Provider for a wire format. baseURL is the API root (https://openrouter.ai/api/v1).
func New(format, baseURL, key, model string) (Provider, error) {
	if model == "" || baseURL == "" {
		return nil, errors.New("a IA não está configurada (modelo e endereço)")
	}
	e := endpoint{base: strings.TrimRight(baseURL, "/"), key: key, model: model, hc: &http.Client{}}
	switch format {
	case FormatChat, "":
		return chat{e}, nil
	case FormatResponses:
		return responses{e}, nil
	case FormatMessages:
		return messages{e}, nil
	}
	return nil, fmt.Errorf("formato de API desconhecido: %q", format)
}

// ValidBaseURL accepts https endpoints and plain http only for a local server (Ollama, LM Studio).
func ValidBaseURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(s) > 200 || strings.ContainsAny(s, " \t\r\n\"'") {
		return errors.New("endereço da API inválido (ex.: https://openrouter.ai/api/v1)")
	}
	switch h := u.Hostname(); {
	case u.Scheme == "https":
	case u.Scheme == "http" && (h == "localhost" || h == "127.0.0.1" || h == "::1"):
	default:
		return errors.New("endereço da API: use https (http só para localhost)")
	}
	return nil
}

// ---- shared HTTP plumbing ----

type endpoint struct {
	base, key, model string
	hc               *http.Client
}

type obj = map[string]any

// stream POSTs body to path and feeds every server-sent event to fn.
func (e endpoint) stream(ctx context.Context, path string, body obj, hdr map[string]string, fn func(data string) error) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", e.base+path, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Title", "BR Panel")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := e.hc.Do(req)
	if err != nil {
		return fmt.Errorf("não consegui falar com a IA: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("a IA respondeu %d: %s", resp.StatusCode, apiError(raw))
	}
	return sse(resp.Body, fn)
}

// apiError pulls the message out of the error bodies of all three APIs.
func apiError(raw []byte) string {
	var v struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &v) == nil && len(v.Error) > 0 {
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(v.Error, &e) == nil && e.Message != "" {
			return e.Message
		}
		var s string
		if json.Unmarshal(v.Error, &s) == nil && s != "" {
			return s
		}
	}
	return strings.TrimSpace(string(raw))
}

// sse reads server-sent events: it ignores comments (": OPENROUTER PROCESSING"), joins multi-line
// data and stops at [DONE].
func sse(r io.Reader, fn func(data string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var data []string
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		d := strings.Join(data, "\n")
		data = data[:0]
		if d == "[DONE]" {
			return io.EOF
		}
		return fn(d)
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if err := flush(); err != nil {
				return stop(err)
			}
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return stop(flush())
}

func stop(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// rawArgs turns the arguments string a model produced into valid JSON; garbage is kept as a JSON
// string so the tool answers with a clear error instead of the whole conversation failing to encode.
func rawArgs(s string) json.RawMessage {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return json.RawMessage(`{}`)
	case json.Valid([]byte(s)):
		return json.RawMessage(s)
	}
	b, _ := json.Marshal(s)
	return b
}

// argsText is the arguments as the APIs that want a string expect them.
func argsText(a json.RawMessage) string {
	if len(a) == 0 {
		return "{}"
	}
	return string(a)
}

// argsObject is the arguments for the API that wants an object.
func argsObject(a json.RawMessage) json.RawMessage {
	if t := strings.TrimSpace(string(a)); strings.HasPrefix(t, "{") {
		return a
	}
	return json.RawMessage(`{}`)
}

func outText(c Call) string {
	if c.Out == "" {
		return "(sem saída)"
	}
	return c.Out
}

func callID(id string, i int) string {
	if id == "" {
		return fmt.Sprintf("call_%d", i)
	}
	return id
}

func bearer(key string) map[string]string {
	if key == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + key}
}
