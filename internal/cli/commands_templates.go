package cli

import (
	"fmt"
	"net/url"
	"strings"
)

func init() {
	handlers["templates list"], handlers["templates show"], handlers["templates install"] = cmdTemplatesList, cmdTemplatesShow, cmdTemplatesInstall
}

func getTemplates(x *call) ([]TemplateInfo, error) {
	var t []TemplateInfo
	err := x.c.getJSON("/api/templates", &t)
	return t, err
}

func cmdTemplatesList(x *call) error {
	ts, err := getTemplates(x)
	if err != nil {
		return err
	}
	if x.json {
		writeJSON(x.out, ts)
		return nil
	}
	rows := [][]string{{"ID", "NOME", "CATEGORIA", "O QUE FAZ"}}
	for _, t := range ts {
		rows = append(rows, []string{t.ID, t.Name, t.Category, t.Summary})
	}
	table(x.out, rows)
	fmt.Fprintln(x.out, "Instalar: brpanel templates install ID --domain SEU.DOMINIO --wait")
	return nil
}

func cmdTemplatesShow(x *call) error {
	ts, err := getTemplates(x)
	if err != nil {
		return err
	}
	var ids []string
	for _, t := range ts {
		ids = append(ids, t.ID)
		if t.ID != strings.ToLower(x.pos[0]) {
			continue
		}
		if x.json {
			writeJSON(x.out, t)
			return nil
		}
		table(x.out, [][]string{{"id:", t.ID}, {"nome:", t.Name}, {"categoria:", t.Category}, {"origem:", t.Source},
			{"versão padrão:", dash(t.Version)}, {"precisa de:", dash(strings.Join(t.Needs, ", "))},
			{"memória sugerida (MB):", fmt.Sprint(t.MemoryMB)}, {"documentação:", dash(t.Docs)}})
		fmt.Fprintf(x.out, "\n%s\n\nDepois de instalar: %s\nInstalar: brpanel templates install %s --domain SEU.DOMINIO --wait\n", t.Description, t.After, t.ID)
		return nil
	}
	return fail(fmt.Sprintf("template %q não existe", x.pos[0]), "DICA: templates disponíveis: "+strings.Join(ids, ", "), "DICA: veja a lista com: brpanel templates list")
}

func cmdTemplatesInstall(x *call) error {
	id := strings.ToLower(x.pos[0])
	form := url.Values{"name": {x.s("name")}, "domain": {x.s("domain")}, "version": {x.s("version")}}
	msg, loc, err := x.c.act("POST", "/templates/"+url.PathEscape(id), form)
	if err != nil {
		if strings.HasPrefix(err.Error(), "não encontrado") {
			return fail(fmt.Sprintf("template %q não existe", id), "DICA: veja os disponíveis com: brpanel templates list")
		}
		return err
	}
	name := strings.TrimPrefix(loc, "/apps/")
	if name == "" || name == loc {
		name = dflt(x.s("name"), id)
	}
	if x.on("wait") {
		if err := waitDeploy(x, name); err != nil {
			return err
		}
		if !x.json && msg != "" {
			fmt.Fprintln(x.out, "PRÓXIMO PASSO: "+afterText(msg))
		}
		return nil
	}
	x.ok(fmt.Sprintf("%q está instalando em segundo plano. Para esperar o fim rode: brpanel apps wait %s", name, name))
	if !x.json && msg != "" {
		fmt.Fprintln(x.out, "PRÓXIMO PASSO: "+afterText(msg))
	}
	return nil
}

// afterText drops the "<Nome> instalando em segundo plano." lead from the panel's message, keeping
// what the template says to do once it runs.
func afterText(msg string) string {
	if _, rest, ok := strings.Cut(msg, "segundo plano. "); ok && rest != "" {
		return rest
	}
	return msg
}
