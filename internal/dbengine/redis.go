package dbengine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type redisEngine struct{}

type redisBins struct{ server, cli string }

func (redisEngine) locate() (any, error) {
	b := redisBins{server: which([]string{"/usr/local/bin"}, "redis-server", "valkey-server"), cli: which([]string{"/usr/local/bin"}, "redis-cli", "valkey-cli")}
	return b, missingList(map[string]string{"redis-server": b.server, "redis-cli": b.cli})
}

func (redisEngine) version(b any) string {
	out, err := runner{}.out(context.Background(), nil, b.(redisBins).server, "--version")
	if err != nil {
		return ""
	}
	return firstVersion(out)
}

func (redisEngine) restoreOffline() bool { return true }
func (redisEngine) backupExt() string    { return "rdb" }
func (redisEngine) extraUnit() []string  { return []string{"TimeoutStopSec=60", "LimitNOFILE=65536"} }

func (redisEngine) init(context.Context, runner, Instance, any, io.Writer) error { return nil }

// configure writes redis.conf (password and bind live here, not in argv).
func (redisEngine) configure(inst Instance, _ any) error {
	conf := fmt.Sprintf(`bind %s
port %d
requirepass %s
dir %s
appendonly no
save 900 1 300 10 60 10000
protected-mode yes
daemonize no
supervised no
logfile ""
pidfile %s
`, inst.BindAddr(), inst.Port, inst.Password, inst.DataDir(), filepath.Join(inst.RunDir(), "redis.pid"))
	return writeOwned(filepath.Join(inst.Dir, "redis.conf"), []byte(conf), 0o600, inst.RunUser)
}

func (redisEngine) execStart(inst Instance, b any) string {
	return b.(redisBins).server + " " + filepath.Join(inst.Dir, "redis.conf")
}

func (redisEngine) postStart(context.Context, runner, Instance, any, io.Writer) error { return nil }

func (redisEngine) url(inst Instance, host string) string {
	return fmt.Sprintf("redis://default:%s@%s:%d", inst.Password, host, inst.Port)
}

func (redisEngine) env(inst Instance) []string { return []string{"REDISCLI_AUTH=" + inst.Password} }

var (
	redisAuthErr  = regexp.MustCompile(`^(NOAUTH|WRONGPASS|AUTH failed|NOPERM|Could not connect|Error:)`)
	redisReplyErr = regexp.MustCompile(`^(ERR |WRONGTYPE|LOADING|MISCONF|READONLY)`)
)

// cli runs redis-cli. It exits 0 even for error replies, so replies are inspected: connection and
// authentication errors always count; ERR-style replies count for administrative commands (a GET
// can legitimately return a string that starts with "ERR ").
func (e redisEngine) cli(ctx context.Context, r runner, inst Instance, b any, args ...string) (string, error) {
	out, err := r.out(ctx, e.env(inst), b.(redisBins).cli, append([]string{"-h", "127.0.0.1", "-p", strconv.Itoa(inst.Port), "--no-auth-warning"}, args...)...)
	if err != nil {
		return "", err
	}
	first := strings.SplitN(out, "\n", 2)[0]
	if redisAuthErr.MatchString(first) {
		return "", errors.New(first)
	}
	if len(args) > 0 {
		switch strings.ToUpper(args[0]) {
		case "CONFIG", "TYPE", "SCAN", "TTL", "PING", "SSCAN":
			if redisReplyErr.MatchString(first) {
				return "", errors.New(first)
			}
		}
	}
	return out, nil
}

func (e redisEngine) backup(ctx context.Context, r runner, inst Instance, b any, dst string, log io.Writer) error {
	return r.run(ctx, log, false, e.env(inst), b.(redisBins).cli, "-h", "127.0.0.1", "-p", strconv.Itoa(inst.Port), "--no-auth-warning", "--rdb", dst)
}

// restore replaces the dataset: the service must be stopped (restoreOffline) so that the old
// append-only files do not win over the restored snapshot.
func (e redisEngine) restore(ctx context.Context, r runner, inst Instance, b any, src string, log io.Writer) error {
	fmt.Fprintf(log, "substituindo os dados por %s\n", filepath.Base(src))
	data := inst.DataDir()
	_ = os.RemoveAll(filepath.Join(data, "appendonlydir"))
	_ = os.Remove(filepath.Join(data, "appendonly.aof"))
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writeOwned(filepath.Join(data, "dump.rdb"), raw, 0o600, inst.RunUser)
}

func (e redisEngine) setPassword(ctx context.Context, r runner, inst Instance, b any, newPassword string) error {
	if _, err := e.cli(ctx, r, inst, b, "CONFIG", "SET", "requirepass", newPassword); err != nil {
		return err
	}
	return nil // redis.conf is rewritten by the manager (configure) with the new password
}

func (e redisEngine) tables(ctx context.Context, r runner, inst Instance, b any) ([]string, error) {
	out, err := e.cli(ctx, r, inst, b, "SCAN", "0", "COUNT", "200")
	if err != nil {
		return nil, err
	}
	lines := splitLines(out)
	if len(lines) > 0 { // first line is the next cursor
		lines = lines[1:]
	}
	return lines, nil
}

func (e redisEngine) preview(ctx context.Context, r runner, inst Instance, b any, key string) (Preview, error) {
	typ, err := e.cli(ctx, r, inst, b, "TYPE", key)
	if err != nil {
		return Preview{}, err
	}
	ttl, _ := e.cli(ctx, r, inst, b, "TTL", key)
	p := Preview{Total: -1}
	get := func(args ...string) []string {
		out, err := e.cli(ctx, r, inst, b, args...)
		if err != nil {
			return nil
		}
		return splitLines(out)
	}
	switch typ {
	case "string":
		p.Columns = []string{"tipo", "ttl", "valor"}
		p.Rows = [][]string{{"string", ttl, strings.Join(get("GET", key), "\n")}}
	case "list":
		p.Columns = []string{"posição", "valor"}
		for i, v := range get("LRANGE", key, "0", "49") {
			p.Rows = append(p.Rows, []string{strconv.Itoa(i), v})
		}
		p.Total, _ = strconv.Atoi(strings.Join(get("LLEN", key), ""))
	case "hash":
		p.Columns = []string{"campo", "valor"}
		kv := get("HGETALL", key)
		for i := 0; i+1 < len(kv); i += 2 {
			p.Rows = append(p.Rows, []string{kv[i], kv[i+1]})
		}
		p.Total, _ = strconv.Atoi(strings.Join(get("HLEN", key), ""))
	case "set":
		p.Columns = []string{"membro"}
		out := get("SSCAN", key, "0", "COUNT", "50")
		if len(out) > 0 {
			out = out[1:]
		}
		for _, v := range out {
			p.Rows = append(p.Rows, []string{v})
		}
		p.Total, _ = strconv.Atoi(strings.Join(get("SCARD", key), ""))
	case "zset":
		p.Columns = []string{"membro", "pontuação"}
		kv := get("ZRANGE", key, "0", "49", "WITHSCORES")
		for i := 0; i+1 < len(kv); i += 2 {
			p.Rows = append(p.Rows, []string{kv[i], kv[i+1]})
		}
		p.Total, _ = strconv.Atoi(strings.Join(get("ZCARD", key), ""))
	default:
		p.Columns = []string{"tipo", "ttl"}
		p.Rows = [][]string{{typ, ttl}}
	}
	return p, nil
}
