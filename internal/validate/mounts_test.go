package validate

import (
	"strings"
	"testing"
)

func TestMount(t *testing.T) {
	const data = "/var/lib/brpanel"
	big := strings.Repeat("x", MaxFileBytes+1)
	for _, tc := range []struct {
		name                           string
		kind, mname, src, dst, content string
		ok                             bool
	}{
		{"volume relativo", "volume", "uploads", "", "storage/uploads", "", true},
		{"volume absoluto", "volume", "dados", "", "/srv/app/dados", "", true},
		{"bind", "bind", "midia", "/srv/midia", "midia", "", true},
		{"arquivo", "file", "conf", "", "config/app.json", `{"a":1}`, true},
		{"arquivo vazio", "file", "vazio", "", ".env.local", "", true},

		{"nome com maiúscula", "volume", "Dados", "", "d", "", false},
		{"nome com espaço", "volume", "meus dados", "", "d", "", false},
		{"nome começa com número", "volume", "1dados", "", "d", "", false},
		{"destino vazio", "volume", "d", "", "", "", false},
		{"destino com ..", "volume", "d", "", "../fora", "", false},
		{"destino com .. no meio", "volume", "d", "", "a/../../b", "", false},
		{"destino ./", "volume", "d", "", "./dados", "", false},
		{"destino com barra dupla", "volume", "d", "", "a//b", "", false},
		{"destino com barra no fim", "volume", "d", "", "dados/", "", false},
		{"destino com hífen inicial", "volume", "d", "", "-dados", "", false},
		{"destino com espaço", "volume", "d", "", "meus dados", "", false},
		{"destino com dois pontos", "volume", "d", "", "a:/etc", "", false},
		{"destino com porcentagem", "volume", "d", "", "a%h", "", false},
		{"destino com cifrão", "volume", "d", "", "a$HOME", "", false},
		{"destino com crase", "volume", "d", "", "a`id`", "", false},
		{"destino com quebra de linha", "volume", "d", "", "a\nExecStartPre=/bin/x", "", false},
		{"destino com NUL", "volume", "d", "", "a\x00b", "", false},
		{"destino /etc", "volume", "d", "", "/etc/cron.d", "", false},
		{"destino /proc", "volume", "d", "", "/proc/x", "", false},
		{"destino /sys", "volume", "d", "", "/sys", "", false},
		{"destino /dev", "volume", "d", "", "/dev/shm", "", false},
		{"destino /boot", "volume", "d", "", "/boot", "", false},
		{"destino /root", "volume", "d", "", "/root/.ssh", "", false},
		{"destino /run", "volume", "d", "", "/run/x", "", false},
		{"destino /usr", "volume", "d", "", "/usr/bin", "", false},
		{"destino raiz", "volume", "d", "", "/", "", false},
		{"destino no diretório de dados", "volume", "d", "", data + "/env", "", false},
		{"destino é o diretório de dados", "volume", "d", "", data, "", false},
		{"destino parecido com /etc é ok", "volume", "d", "", "/etcetera/x", "", true},
		{"destino parecido com o dir de dados é ok", "volume", "d", "", data + "-extra/x", "", true},

		{"bind sem origem", "bind", "m", "", "m", "", false},
		{"bind com origem relativa", "bind", "m", "srv/midia", "m", "", false},
		{"bind com ..", "bind", "m", "/srv/../etc", "m", "", false},
		{"bind em /etc", "bind", "m", "/etc", "m", "", false},
		{"bind em /root/x", "bind", "m", "/root/x", "m", "", false},
		{"bind em /run", "bind", "m", "/run/docker.sock", "m", "", false},
		{"bind no banco do painel", "bind", "m", data + "/panel.db", "m", "", false},
		{"bind na raiz", "bind", "m", "/", "m", "", false},
		{"bind com espaço", "bind", "m", "/srv/a b", "m", "", false},
		{"bind com conteúdo", "bind", "m", "/srv/x", "m", "x", false},
		{"volume com origem", "volume", "m", "/srv/x", "m", "", false},
		{"arquivo com origem", "file", "m", "/srv/x", "m", "", false},
		{"arquivo grande demais", "file", "m", "", "m", big, false},
		{"arquivo com NUL", "file", "m", "", "m", "a\x00b", false},
		{"tipo desconhecido", "tmpfs", "m", "", "m", "", false},
	} {
		err := Mount(tc.kind, tc.mname, tc.src, tc.dst, tc.content, data)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}

func TestDeniedPathWithoutDataDir(t *testing.T) {
	if DeniedPath("/srv/x", "") {
		t.Error("sem diretório de dados só valem as pastas do sistema")
	}
	if !DeniedPath("/etc/passwd", "") || !DeniedPath("/", "") {
		t.Error("pastas do sistema continuam protegidas")
	}
}

func TestMountEnvName(t *testing.T) {
	if got := MountEnvName("meus-dados"); got != "VOLUME_MEUS_DADOS" {
		t.Errorf("got %s", got)
	}
}
