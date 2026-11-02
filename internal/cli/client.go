package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// client talks to the panel's normal routes; the socket side authenticates every request.
type client struct {
	hc     *http.Client
	base   string // http://brpanel for the socket, the server URL for a remote
	remote bool
}

type reply struct {
	status int
	loc    string
	body   []byte
}

func (c *client) do(method, path string, form url.Values) (reply, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		return reply{}, fail("endereço inválido: " + path)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		if c.remote {
			err = explain(err, c.base)
		}
		return reply{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return reply{resp.StatusCode, resp.Header.Get("Location"), b}, nil
}

// check turns the replies that mean "this did not work" into errors an agent can act on.
func check(r reply) error {
	switch {
	case r.status == http.StatusSeeOther && strings.HasPrefix(r.loc, "/setup"):
		return fail("o painel ainda não foi configurado", "DICA: abra o painel no navegador e conclua a configuração inicial (/setup)")
	case r.status == http.StatusSeeOther && strings.HasPrefix(r.loc, "/login"):
		return fail("o painel recusou a sessão local", "DICA: reinicie o painel (systemctl restart brpanel) e tente de novo")
	case r.status == http.StatusUnauthorized:
		return fail("o servidor recusou o token (inválido ou revogado)",
			"DICA: crie outro no painel (aba CLI > Acesso remoto) ou no servidor com: sudo brpanel tokens create --name NOME",
			"DICA: depois entre de novo: brpanel login ENDEREÇO")
	case r.status == http.StatusTooManyRequests:
		return fail("muitas tentativas recusadas; o servidor bloqueou este IP por um minuto", "DICA: espere 1 minuto e tente de novo")
	case r.status == http.StatusNotFound:
		return fail("não encontrado", "DICA: confira o nome com: brpanel apps list  (ou brpanel dbs list)")
	case r.status == http.StatusUnprocessableEntity:
		var e struct{ Error string }
		if json.Unmarshal(r.body, &e) == nil && e.Error != "" {
			return fail(e.Error, "DICA: corrija o valor indicado e rode o comando de novo")
		}
	case r.status >= 400:
		return fail(fmt.Sprintf("o painel respondeu %d: %s", r.status, strings.TrimSpace(string(r.body))))
	}
	return nil
}

// get fetches a path and returns the body (JSON or text).
func (c *client) get(path string) ([]byte, error) {
	r, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if err := check(r); err != nil {
		return nil, err
	}
	return r.body, nil
}

func (c *client) getJSON(path string, v any) error {
	b, err := c.get(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fail("resposta inesperada do painel em " + path)
	}
	return nil
}

// failurePrefixes start the flash messages the panel uses for "did not happen".
var failurePrefixes = []string{"Falha", "Não ", "O Nginx recusou", "Aguarde", "Já existe"}

func failed(msg string) bool {
	for _, p := range failurePrefixes {
		if strings.HasPrefix(msg, p) {
			return true
		}
	}
	return false
}

// act sends a form and reads the outcome the panel puts in the redirect (?msg=…). msg may be empty
// when the panel redirects without a message; loc is where it redirected to.
func (c *client) act(method, path string, form url.Values) (msg, loc string, err error) {
	if form == nil {
		form = url.Values{}
	}
	r, err := c.do(method, path, form)
	if err != nil {
		return "", "", err
	}
	if err := check(r); err != nil {
		return "", "", err
	}
	if u, perr := url.Parse(r.loc); perr == nil {
		msg, loc = u.Query().Get("msg"), u.Path
	}
	if failed(msg) {
		return "", loc, fail(msg)
	}
	return msg, loc, nil
}
