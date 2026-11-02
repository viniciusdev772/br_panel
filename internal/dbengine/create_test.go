package dbengine

import (
	"strings"
	"testing"
)

func TestColumnDDL(t *testing.T) {
	q := func(s string) string { return `"` + s + `"` }
	got, err := columnDDL("id id\nnome texto, idade inteiro", 0, q)
	if err != nil || got != `"id" bigserial PRIMARY KEY, "nome" text, "idade" integer` {
		t.Fatalf("pg: %q, %v", got, err)
	}
	if got, _ := columnDDL("", 1, q); got != `"id" BIGINT AUTO_INCREMENT PRIMARY KEY` {
		t.Fatalf("mysql default: %q", got)
	}
	for _, bad := range []string{"x text); DROP TABLE a; --", "nome varchar", "a texto\nA inteiro", "1a texto", "so_nome"} {
		if _, err := columnDDL(bad, 0, q); err == nil {
			t.Errorf("%q deveria ser rejeitado", bad)
		}
	}
	if _, err := columnDDL(strings.Repeat("c texto,", 101), 0, q); err == nil {
		t.Error("mais de 100 colunas deveria falhar")
	}
}
