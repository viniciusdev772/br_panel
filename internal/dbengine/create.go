package dbengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	maxCreateValue = 64 << 10 // bytes accepted for a key value / JSON document
	maxRedisKey    = 512
)

// CreateItem adds a new item to a database from the "Dados" tab: a table (PostgreSQL/MySQL, value =
// column list), a collection (MongoDB, value = optional JSON document) or a string key (Redis,
// value = the content). It refuses to overwrite an existing item.
func (m *Manager) CreateItem(ctx context.Context, name, item, value string) error {
	if len(value) > maxCreateValue {
		return errors.New("conteúdo grande demais (máximo 64 KB)")
	}
	inst, e, bins, err := m.get(name)
	if err != nil {
		return err
	}
	return e.create(ctx, m.runnerFor(inst), inst, bins, strings.TrimSpace(item), value)
}

// sqlTypes maps the friendly column types shown in the UI to each SQL dialect. Only these reach the
// CREATE TABLE statement, so nothing typed by the user is ever concatenated into SQL except
// identifiers validated by identRe.
var sqlTypes = map[string][2]string{ // type -> {postgres, mysql}
	"texto":     {"text", "TEXT"},
	"inteiro":   {"integer", "INT"},
	"decimal":   {"numeric(20,6)", "DECIMAL(20,6)"},
	"booleano":  {"boolean", "BOOLEAN"},
	"data":      {"date", "DATE"},
	"data_hora": {"timestamptz", "DATETIME"},
	"json":      {"jsonb", "JSON"},
	"id":        {"bigserial PRIMARY KEY", "BIGINT AUTO_INCREMENT PRIMARY KEY"},
}

// columnDDL turns "nome texto" / "id id" lines (or comma separated) into column definitions.
// An empty spec yields a single auto-increment id column. dialect is 0 for PostgreSQL, 1 for MySQL.
func columnDDL(spec string, dialect int, quote func(string) string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = "id id"
	}
	var cols []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(spec, func(r rune) bool { return r == '\n' || r == ',' }) {
		f := strings.Fields(part)
		if len(f) != 2 || !identRe.MatchString(f[0]) {
			return "", fmt.Errorf("coluna inválida %q: use \"nome tipo\" (tipos: texto, inteiro, decimal, booleano, data, data_hora, json, id)", strings.TrimSpace(part))
		}
		typ, ok := sqlTypes[strings.ToLower(f[1])]
		if !ok {
			return "", fmt.Errorf("tipo %q desconhecido (use texto, inteiro, decimal, booleano, data, data_hora, json ou id)", f[1])
		}
		key := strings.ToLower(f[0])
		if seen[key] {
			return "", fmt.Errorf("coluna %q repetida", f[0])
		}
		seen[key] = true
		cols = append(cols, quote(f[0])+" "+typ[dialect])
	}
	if len(cols) > 100 {
		return "", errors.New("colunas demais (máximo 100)")
	}
	return strings.Join(cols, ", "), nil
}

func (e pgEngine) create(ctx context.Context, r runner, inst Instance, b any, item, value string) error {
	schema, table, ok := strings.Cut(item, ".")
	if !ok {
		schema, table = "public", item
	}
	if !identRe.MatchString(schema) || !identRe.MatchString(table) {
		return errors.New("nome de tabela inválido (letras, números e _; começa com letra)")
	}
	cols, err := columnDDL(value, 0, func(s string) string { return `"` + s + `"` })
	if err != nil {
		return err
	}
	_, err = e.psql(ctx, r, inst, b, inst.DBName, "-c", fmt.Sprintf(`CREATE TABLE "%s"."%s" (%s)`, schema, table, cols))
	return err
}

func (e mysqlEngine) create(ctx context.Context, r runner, inst Instance, b any, item, value string) error {
	if !identRe.MatchString(item) {
		return errors.New("nome de tabela inválido (letras, números e _; começa com letra)")
	}
	cols, err := columnDDL(value, 1, func(s string) string { return "`" + s + "`" })
	if err != nil {
		return err
	}
	_, err = e.query(ctx, r, inst, b, fmt.Sprintf("CREATE TABLE `%s` (%s)", item, cols))
	return err
}

func (e mongoEngine) create(ctx context.Context, r runner, inst Instance, b any, item, value string) error {
	if !collRe.MatchString(item) {
		return errors.New("nome de coleção inválido (letras, números, _ . -)")
	}
	insert := ""
	if v := strings.TrimSpace(value); v != "" {
		var doc map[string]any
		if err := json.Unmarshal([]byte(v), &doc); err != nil || doc == nil {
			return errors.New("o documento precisa ser um objeto JSON, ex.: {\"nome\": \"Ana\"}")
		}
		insert = fmt.Sprintf("c.insertOne(EJSON.parse(%s));", jsonStr(v))
	}
	_, err := e.eval(ctx, r, inst, b, e.authURI(inst), fmt.Sprintf(
		`if (db.getCollectionNames().includes(%[1]s)) { throw new Error("a coleção já existe"); }
db.createCollection(%[1]s); const c = db.getCollection(%[1]s); %[2]s`, jsonStr(item), insert))
	return err
}

func (e redisEngine) create(ctx context.Context, r runner, inst Instance, b any, key, value string) error {
	if key == "" || len(key) > maxRedisKey || strings.ContainsAny(key, "\x00\r\n") {
		return errors.New("chave inválida")
	}
	// The value travels on stdin (-x), never argv, where other local users could read it with ps.
	// SETNX refuses to overwrite an existing key.
	c := r.command(ctx, false, e.env(inst), b.(redisBins).cli,
		"-h", "127.0.0.1", "-p", strconv.Itoa(inst.Port), "--no-auth-warning", "-x", "SETNX", key)
	c.Stdin = strings.NewReader(value)
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := c.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	switch out := strings.TrimSpace(stdout.String()); {
	case out == "1" || strings.HasSuffix(out, "(integer) 1"):
		return nil
	case out == "0" || strings.HasSuffix(out, "(integer) 0"):
		return errors.New("a chave já existe")
	default:
		return errors.New(out)
	}
}
