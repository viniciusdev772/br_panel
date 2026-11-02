package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func init() {
	handlers["login"], handlers["logout"], handlers["whoami"] = cmdLogin, cmdLogout, cmdWhoami
	handlers["tokens list"], handlers["tokens create"], handlers["tokens revoke"] = cmdTokensList, cmdTokensCreate, cmdTokensRevoke
}

func cmdLogin(x *call) error {
	in := bufio.NewReader(x.in)
	server, byStdin := "", x.on("token-stdin")
	if len(x.pos) > 0 {
		server = x.pos[0]
	}
	if server == "" {
		if byStdin {
			return usageErr(x.cmd, "com --token-stdin o servidor precisa vir como argumento")
		}
		server = prompt(in, x.in, "Endereço do servidor (IP ou domínio): ", false)
	}
	base, plain, err := normalizeServer(server)
	if err != nil {
		return usageErr(x.cmd, err.Error())
	}
	if plain && !x.on("allow-http") {
		if byStdin || !isTerminal(x.in) {
			return fail("o servidor "+base+" não usa SSL: o token viajaria sem criptografia",
				"DICA: o melhor é publicar o painel em um domínio com SSL e usar https://DOMINIO",
				"DICA: se a rede é de confiança (VPN, rede local), repita com --allow-http")
		}
		ans := strings.ToLower(prompt(in, x.in, "AVISO: "+base+" não usa SSL; o token viajaria sem criptografia. Continuar? [s/N]: ", false))
		if ans != "s" && ans != "sim" && ans != "y" && ans != "yes" {
			return fail("cancelado")
		}
	}
	token := ""
	if byStdin {
		line, _ := in.ReadString('\n')
		token = strings.TrimSpace(line)
	} else {
		token = prompt(in, x.in, "Token (começa com brp_; crie na aba CLI do painel): ", true)
	}
	if !strings.HasPrefix(token, "brp_") {
		return usageErr(x.cmd, "token inválido: ele começa com brp_ (crie um na aba CLI do painel ou com: sudo brpanel tokens create --name NOME)")
	}
	next := x.rt
	if next == nil {
		next = http.DefaultTransport
	}
	if _, err := newClient(bearerRT{token, next}, base, true).get("/api/overview"); err != nil {
		return err
	}
	if err := saveConfig(config{Server: base, Token: token}); err != nil {
		return fail("conectou, mas não consegui salvar o login: " + err.Error())
	}
	x.ok("conectado a " + base + ". Agora os comandos controlam esse servidor. Teste com: brpanel status")
	if plain {
		fmt.Fprintln(os.Stderr, "AVISO: este servidor não usa SSL; evite redes não confiáveis.")
	}
	return nil
}

func cmdLogout(x *call) error {
	p, err := configPath()
	if err != nil {
		return fail("não achei onde o login fica salvo: " + err.Error())
	}
	if _, ok := loadConfig(); !ok {
		x.ok("nenhum login remoto salvo; os comandos já usam o socket local.")
		return nil
	}
	if err := os.Remove(p); err != nil {
		return fail("não consegui apagar o login salvo: " + err.Error())
	}
	x.ok("login remoto removido. Os comandos voltam a usar o socket local. O token continua válido no servidor; para revogá-lo: brpanel tokens revoke ID --yes")
	return nil
}

func cmdWhoami(x *call) error {
	cfg, ok := loadConfig()
	if !ok {
		if x.json {
			writeJSON(x.out, map[string]any{"mode": "local", "socket": DefaultData + "/" + SocketName})
			return nil
		}
		fmt.Fprintf(x.out, "modo: local (socket %s/%s; precisa de root)\nPara controlar outro servidor: brpanel login ENDEREÇO\n", DefaultData, SocketName)
		return nil
	}
	masked := cfg.Token[:min(8, len(cfg.Token))] + "…"
	if x.json {
		writeJSON(x.out, map[string]any{"mode": "remote", "server": cfg.Server, "token": masked})
		return nil
	}
	fmt.Fprintf(x.out, "modo: remoto\nservidor: %s\ntoken: %s\n(para usar o socket deste servidor mesmo assim: acrescente --local; para sair: brpanel logout)\n", cfg.Server, masked)
	return nil
}

func cmdTokensList(x *call) error {
	var toks []struct {
		ID       int64     `json:"id"`
		Name     string    `json:"name"`
		Created  time.Time `json:"created"`
		LastUsed time.Time `json:"last_used"`
	}
	if err := x.c.getJSON("/api/tokens", &toks); err != nil {
		return err
	}
	if x.json {
		writeJSON(x.out, toks)
		return nil
	}
	if len(toks) == 0 {
		fmt.Fprintln(x.out, "Nenhum token. Crie um com: sudo brpanel tokens create --name notebook")
		return nil
	}
	rows := [][]string{{"ID", "NOME", "CRIADO", "ÚLTIMO USO"}}
	for _, t := range toks {
		used := "nunca"
		if !t.LastUsed.IsZero() && t.LastUsed.Unix() > 0 {
			used = t.LastUsed.Local().Format("2006-01-02 15:04")
		}
		rows = append(rows, []string{strconv.FormatInt(t.ID, 10), t.Name, t.Created.Local().Format("2006-01-02 15:04"), used})
	}
	table(x.out, rows)
	return nil
}

func cmdTokensCreate(x *call) error {
	if !x.set("name") {
		return usageErr(x.cmd, "faltou --name (letras minúsculas, números e hífen, ex.: notebook)")
	}
	r, err := x.c.do("POST", "/cli/tokens", url.Values{"name": {x.s("name")}})
	if err != nil {
		return err
	}
	if err := check(r); err != nil {
		return err
	}
	var out struct{ Token string }
	if json.Unmarshal(r.body, &out) != nil || out.Token == "" {
		return fail("o painel não devolveu o token")
	}
	if x.json {
		writeJSON(x.out, map[string]any{"ok": true, "name": x.s("name"), "token": out.Token})
		return nil
	}
	fmt.Fprintf(x.out, "OK: token %q criado. Copie agora; ele não aparece de novo:\n%s\n", x.s("name"), out.Token)
	fmt.Fprintf(x.out, "No outro computador: printf '%%s' '%s' | brpanel login SERVIDOR --token-stdin\n", out.Token)
	return nil
}

func cmdTokensRevoke(x *call) error {
	if _, err := strconv.ParseInt(x.pos[0], 10, 64); err != nil {
		return usageErr(x.cmd, "o ID é um número; veja os IDs com: brpanel tokens list")
	}
	msg, _, err := x.c.act("POST", "/cli/tokens/"+x.pos[0]+"/revoke", nil)
	if err != nil {
		return err
	}
	x.ok(dflt(msg, "Token revogado."))
	return nil
}
