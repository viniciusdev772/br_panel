// Package validate holds the input validators for everything that ends up in a
// shell command, a systemd unit or an Nginx file.
package validate

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	nameRe    = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)
	branchRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$`)
	envKeyRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	domainRe  = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)
	scpRepoRe = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9._/~-]+$`)
)

func Name(n string) error {
	if !nameRe.MatchString(n) {
		return errors.New("nome inválido: use 2-32 caracteres (a-z, 0-9, -), começando com letra")
	}
	return nil
}

// Repo accepts only https://, ssh:// and scp-style git@host:path URLs.
// Credentials in the URL are rejected; private repos should use an SSH deploy key.
func Repo(raw string) error {
	if raw == "" || len(raw) > 300 {
		return errors.New("URL do repositório inválida")
	}
	if strings.ContainsAny(raw, " \t\r\n\x00") || strings.HasPrefix(raw, "-") {
		return errors.New("URL do repositório contém caracteres inválidos")
	}
	if scpRepoRe.MatchString(raw) {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Path == "" || u.Path == "/" {
		return errors.New("URL do repositório inválida")
	}
	if u.Scheme != "https" && u.Scheme != "ssh" {
		return errors.New("use uma URL https://, ssh:// ou git@host:caminho")
	}
	if u.User != nil && (u.Scheme == "https" || u.User.String() != "git") {
		return errors.New("não coloque credenciais na URL; use chave de deploy SSH")
	}
	return nil
}

func Branch(b string) error {
	if !branchRe.MatchString(b) || strings.Contains(b, "..") || strings.HasSuffix(b, ".lock") || strings.HasSuffix(b, "/") {
		return errors.New("branch inválida")
	}
	return nil
}

func Command(label, c string, required bool) error {
	if c == "" {
		if required {
			return fmt.Errorf("%s é obrigatório", label)
		}
		return nil
	}
	if len(c) > 500 || strings.ContainsAny(c, "\r\n\x00") {
		return fmt.Errorf("%s deve ter uma única linha (até 500 caracteres)", label)
	}
	return nil
}

func Port(p int) error {
	if p != 0 && (p < 1024 || p > 65535) {
		return errors.New("porta deve ser 0 (nenhuma) ou entre 1024 e 65535")
	}
	return nil
}

// Scale limits for an app's instances and resources (see store.App).
const (
	MaxReplicas = 16
	MaxMemoryMB = 1 << 20 // 1 TiB, only a sanity bound
	MaxCPUPct   = 6400    // 64 CPUs
	MaxRateRPS  = 10000
)

// Scale checks the instance count and the optional limits. Zero means "no limit" for the limits
// and is not allowed for replicas.
func Scale(replicas, memoryMB, cpuPercent, rateLimit int) error {
	switch {
	case replicas < 1 || replicas > MaxReplicas:
		return fmt.Errorf("instâncias deve ficar entre 1 e %d", MaxReplicas)
	case memoryMB != 0 && (memoryMB < 32 || memoryMB > MaxMemoryMB):
		return fmt.Errorf("memória máxima deve ser 0 (sem limite) ou entre 32 e %d MB", MaxMemoryMB)
	case cpuPercent != 0 && (cpuPercent < 5 || cpuPercent > MaxCPUPct):
		return fmt.Errorf("CPU máxima deve ser 0 (sem limite) ou entre 5%% e %d%%", MaxCPUPct)
	case rateLimit < 0 || rateLimit > MaxRateRPS:
		return fmt.Errorf("limite de requisições deve ser 0 (desligado) ou até %d por segundo", MaxRateRPS)
	}
	return nil
}

// AutoScale checks the autoscaling settings of an app. memoryMB is the app's memory limit per
// instance: a memory target needs it, because utilization is measured against that limit.
func AutoScale(enabled bool, min, max, targetCPU, targetMem, outCooldown, inCooldown, memoryMB int) error {
	if !enabled {
		return nil
	}
	switch {
	case min < 1 || max < min || max > MaxReplicas:
		return fmt.Errorf("escalonamento automático: o mínimo deve ser de 1 até o máximo, e o máximo no máximo %d", MaxReplicas)
	case targetCPU < 10 || targetCPU > 95:
		return errors.New("escalonamento automático: a meta de CPU deve ficar entre 10% e 95%")
	case targetMem != 0 && (targetMem < 10 || targetMem > 95):
		return errors.New("escalonamento automático: a meta de memória deve ficar entre 10% e 95%")
	case targetMem != 0 && memoryMB == 0:
		return errors.New("escalonamento automático: a meta de memória exige o limite de memória por instância")
	case outCooldown < 0 || outCooldown > 3600 || inCooldown < 0 || inCooldown > 3600:
		return errors.New("escalonamento automático: os tempos de espera vão de 0 a 3600 segundos")
	}
	return nil
}

// ParseEnv parses KEY=VALUE lines (blank lines and # comments allowed).
func ParseEnv(text string) ([][2]string, error) {
	var out [][2]string
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || !envKeyRe.MatchString(k) {
			return nil, fmt.Errorf("variáveis de ambiente, linha %d: use CHAVE=valor", i+1)
		}
		if strings.ContainsRune(v, 0) {
			return nil, fmt.Errorf("variáveis de ambiente, linha %d: valor inválido", i+1)
		}
		out = append(out, [2]string{k, v})
	}
	if len(out) > 100 {
		return nil, errors.New("máximo de 100 variáveis de ambiente")
	}
	return out, nil
}

// Domain accepts a lowercase DNS hostname (no IPs, wildcards or ports).
func Domain(d string) error {
	if d == "" {
		return nil
	}
	if len(d) > 253 || !domainRe.MatchString(d) {
		return errors.New("domínio inválido (ex.: app.exemplo.com.br)")
	}
	return nil
}
