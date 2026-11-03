package cli

import (
	"fmt"
	"net/url"
)

func init() {
	handlers["cache status"], handlers["cache prune"], handlers["cache clear"] = cmdCacheStatus, cmdCachePrune, cmdCacheClear
}

func cmdCacheStatus(x *call) error {
	var info CacheInfo
	if err := x.c.getJSON("/api/cache", &info); err != nil {
		return err
	}
	if x.json {
		writeJSON(x.out, info)
		return nil
	}
	rows := [][]string{{"FERRAMENTA", "TAMANHO"}}
	for _, it := range info.Items {
		rows = append(rows, []string{it.Label, humanBytes(it.Bytes)})
	}
	table(x.out, rows)
	fmt.Fprintf(x.out, "Total: %s em %s\n", humanBytes(info.Total), info.Dir)
	fmt.Fprintln(x.out, "Limpar o que ninguém usa há 30 dias: brpanel cache prune   |   esvaziar tudo: brpanel cache clear --yes")
	return nil
}

func cmdCachePrune(x *call) error {
	form := url.Values{}
	if x.set("days") {
		days, _, err := x.num("days")
		if err != nil {
			return err
		}
		form.Set("days", fmt.Sprint(days))
	}
	msg, _, err := x.c.act("POST", "/cache/prune", form)
	if err != nil {
		return err
	}
	x.ok(msg)
	return nil
}

func cmdCacheClear(x *call) error {
	msg, _, err := x.c.act("POST", "/cache/clear", nil)
	if err != nil {
		return err
	}
	x.ok(msg)
	return nil
}
