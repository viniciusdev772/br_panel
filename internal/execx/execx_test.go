package execx

import (
	"context"
	"strings"
	"testing"
)

func TestRunLogsCommandAndOutput(t *testing.T) {
	var log strings.Builder
	if err := Run(context.Background(), &log, "echo", "olá", "mundo"); err != nil {
		t.Fatal(err)
	}
	if got := log.String(); got != "$ echo olá mundo\nolá mundo\n" {
		t.Errorf("log: %q", got)
	}
}

func TestRunDoesNotUseShell(t *testing.T) {
	var log strings.Builder
	if err := Run(context.Background(), &log, "echo", "a; echo injetado"); err != nil {
		t.Fatal(err)
	}
	if strings.Count(log.String(), "injetado") != 2 { // once in the echoed command line, once as echo's literal argument
		t.Errorf("o argumento deve ser literal, não interpretado: %q", log.String())
	}
}

func TestRunReportsFailure(t *testing.T) {
	var log strings.Builder
	if err := Run(context.Background(), &log, "sh", "-c", "echo falhou >&2; exit 3"); err == nil {
		t.Error("esperava erro")
	}
	if !strings.Contains(log.String(), "falhou") {
		t.Errorf("stderr deve ir para o log: %q", log.String())
	}
	if err := Run(context.Background(), &log, "programa-que-nao-existe-xyz"); err == nil {
		t.Error("esperava erro de execução")
	}
}
