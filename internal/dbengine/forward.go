package dbengine

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/viniciusdev772/br_panel/internal/store"
	"github.com/viniciusdev772/br_panel/internal/systemd"
)

// A "forward" makes the engine's standard port on loopback (3306, 5432, 27017, 6379) reach a
// managed database that lives on another port. It serves projects that hardcode localhost:3306
// in their source: no code change, any language. It is a systemd socket plus
// systemd-socket-proxyd, so it survives reboots and starts the database on first use.

// StandardPort is the port clients assume for an engine when nothing else is configured.
func StandardPort(k Kind) int {
	switch k {
	case Postgres:
		return 5432
	case MySQL:
		return 3306
	case MongoDB:
		return 27017
	case Redis:
		return 6379
	}
	return 0
}

// ForwardInfo is what the database page shows about its forward.
type ForwardInfo struct {
	Port     int  // standard port that would be forwarded
	Active   bool // the forward units exist
	Possible bool // false when the database already listens on its standard port
}

func fwdUnitBase(db string) string { return "brpanelfwd-" + db }

func (m *Manager) fwdFile(db, ext string) string {
	return filepath.Join(m.Settings().UnitDir, fwdUnitBase(db)+ext)
}

// ForwardState reports the forward of a database. Whether it is active is read from the unit
// files themselves, so there is no second source of truth to drift.
func (m *Manager) ForwardState(d store.Database) ForwardInfo {
	std := StandardPort(Kind(d.Kind))
	_, err := os.Stat(m.fwdFile(d.Name, ".socket"))
	return ForwardInfo{Port: std, Active: err == nil, Possible: std != 0 && std != d.Port}
}

// renderForward returns the .socket and .service texts. ipv6 adds [::1], which fails to bind on
// hosts without IPv6 loopback.
func renderForward(db string, listen, target int, proxyd string, ipv6 bool) (socket, service string) {
	socket = fmt.Sprintf("# Gerado pelo BR Panel — não edite.\n[Unit]\nDescription=BR Panel: 127.0.0.1:%d -> banco %s (porta %d)\n\n[Socket]\nListenStream=127.0.0.1:%d\n", listen, db, target, listen)
	if ipv6 {
		socket += fmt.Sprintf("ListenStream=[::1]:%d\n", listen)
	}
	socket += "\n[Install]\nWantedBy=sockets.target\n"
	service = fmt.Sprintf("# Gerado pelo BR Panel — não edite.\n[Unit]\nDescription=BR Panel: repasse %d -> 127.0.0.1:%d (banco %s)\nRequires=%s\nAfter=%s\n\n"+
		"[Service]\nExecStart=%s 127.0.0.1:%d\nPrivateTmp=yes\nDynamicUser=yes\nNoNewPrivileges=yes\n",
		listen, target, db, systemd.DBUnitName(db), systemd.DBUnitName(db), proxyd, target)
	return socket, service
}

// findProxyd locates systemd-socket-proxyd (part of the systemd package).
func findProxyd() (string, error) {
	for _, p := range []string{"/usr/lib/systemd/systemd-socket-proxyd", "/lib/systemd/systemd-socket-proxyd", "/usr/libexec/systemd/systemd-socket-proxyd"} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("systemd-socket-proxyd não encontrado (vem com o pacote systemd)")
}

func canListen(network, addr string) bool {
	l, err := net.Listen(network, addr)
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// EnableForward exposes the engine's standard port on loopback and sends it to the database.
func (m *Manager) EnableForward(name string) error {
	d, ok := m.Store.GetDatabase(name)
	if !ok {
		return errors.New("banco não encontrado")
	}
	proxyd, err := findProxyd()
	if err != nil {
		return err
	}
	return m.enableForward(d, StandardPort(Kind(d.Kind)), proxyd)
}

// enableForward is EnableForward with the port and proxy binary given (tests use a free port).
func (m *Manager) enableForward(d store.Database, listen int, proxyd string) error {
	switch {
	case listen == 0:
		return errors.New("tipo de banco sem porta padrão")
	case listen == d.Port:
		return fmt.Errorf("o banco já usa a porta padrão (%d); não há o que repassar", listen)
	}
	already := m.ForwardState(d).Active
	if !already {
		if m.Store.UsedPorts()[listen] {
			return fmt.Errorf("a porta %d já é usada por outro app ou banco do painel", listen)
		}
		if !canListen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(listen))) {
			return fmt.Errorf("a porta %d em 127.0.0.1 já está ocupada (outro serviço ou outro repasse)", listen)
		}
	}
	socket, service := renderForward(d.Name, listen, d.Port, proxyd, canListen("tcp6", net.JoinHostPort("::1", "0")))
	for ext, body := range map[string]string{".socket": socket, ".service": service} {
		if err := os.WriteFile(m.fwdFile(d.Name, ext), []byte(body), 0o644); err != nil {
			return err
		}
	}
	_, _ = systemd.Ctl("daemon-reload")
	if out, err := systemd.Ctl("enable", "--now", fwdUnitBase(d.Name)+".socket"); err != nil {
		m.removeForwardFiles(d.Name)
		return fmt.Errorf("não consegui ativar o repasse: %s", out)
	}
	m.record("database", d.Name, fmt.Sprintf("Repasse ativado: 127.0.0.1:%d → %d", listen, d.Port))
	return nil
}

func (m *Manager) removeForwardFiles(name string) {
	for _, ext := range []string{".socket", ".service"} {
		_ = os.Remove(m.fwdFile(name, ext))
	}
	_, _ = systemd.Ctl("daemon-reload")
}

// DisableForward stops and removes the forward. It is a no-op when there is none.
func (m *Manager) DisableForward(name string) error {
	if _, err := os.Stat(m.fwdFile(name, ".socket")); err != nil {
		return nil
	}
	_, _ = systemd.Ctl("disable", "--now", fwdUnitBase(name)+".socket")
	_, _ = systemd.Ctl("stop", fwdUnitBase(name)+".service")
	m.removeForwardFiles(name)
	m.record("database", name, "Repasse desativado")
	return nil
}
