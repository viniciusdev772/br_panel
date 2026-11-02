#!/bin/sh
# Instala (ou atualiza) o BR Panel como serviço systemd, a partir do binário publicado nas Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/viniciusdev772/br_panel/main/scripts/install.sh | sudo sh
#   ... | sudo sh -s -- --with-nginx          # também instala nginx e certbot (domínios e SSL)
#   ... | sudo sh -s -- --version v0.1.0      # uma versão específica em vez da última
#
# Rodar de novo atualiza o binário (o anterior fica em brpanel.bak) e preserva a unit e os dados.
set -eu

REPO="viniciusdev772/br_panel"
BIN=/usr/local/bin/brpanel
UNIT=/etc/systemd/system/brpanel.service
VERSION=latest
WITH_NGINX=0

die() { echo "erro: $*" >&2; exit 1; }
say() { echo "==> $*"; }

while [ $# -gt 0 ]; do
	case "$1" in
	--version) [ $# -ge 2 ] || die "--version precisa de um valor (ex.: v0.1.0)"; VERSION=$2; shift 2 ;;
	--with-nginx) WITH_NGINX=1; shift ;;
	-h | --help) sed -n '2,8p' "$0" 2>/dev/null || true; exit 0 ;;
	*) die "opção desconhecida: $1" ;;
	esac
done

[ "$(uname -s)" = Linux ] || die "o BR Panel só roda em Linux"
[ "$(id -u)" -eq 0 ] || die "rode como root (ex.: sudo sh install.sh)"
command -v systemctl >/dev/null 2>&1 || die "systemd não encontrado: o painel gerencia as aplicações com systemd"

case "$(uname -m)" in
x86_64 | amd64) ARCH=amd64 ;;
aarch64 | arm64) ARCH=arm64 ;;
*) die "arquitetura não suportada: $(uname -m) (use amd64 ou arm64)" ;;
esac

apt_install() {
	command -v apt-get >/dev/null 2>&1 || die "instale manualmente: $* (apt-get não encontrado)"
	DEBIAN_FRONTEND=noninteractive apt-get update -qq
	DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "$@"
}

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO "$2" "$1"; }
else
	apt_install curl
	fetch() { curl -fsSL "$1" -o "$2"; }
fi

command -v git >/dev/null 2>&1 || { say "instalando git (o painel clona os repositórios com ele)"; apt_install git; }
if [ "$WITH_NGINX" -eq 1 ]; then
	say "instalando nginx e certbot"
	apt_install nginx certbot
fi

NAME="brpanel-linux-$ARCH"
if [ "$VERSION" = latest ]; then
	BASE="https://github.com/$REPO/releases/latest/download"
else
	BASE="https://github.com/$REPO/releases/download/$VERSION"
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

say "baixando $NAME ($VERSION)"
fetch "$BASE/$NAME" "$TMP/$NAME" || die "não consegui baixar $BASE/$NAME (a versão existe?)"
fetch "$BASE/$NAME.sha256" "$TMP/$NAME.sha256" || die "não consegui baixar a soma de verificação"
(cd "$TMP" && sha256sum -c "$NAME.sha256" >/dev/null) || die "a soma SHA-256 do binário não confere; instalação cancelada"

UPGRADE=0
if [ -x "$BIN" ]; then
	UPGRADE=1
	cp -p "$BIN" "$BIN.bak"
fi
install -m 0755 "$TMP/$NAME" "$BIN"

# A unit só é criada na primeira vez: quem já a editou (ex.: -addr 127.0.0.1:8080 depois do SSL) não perde a mudança.
if [ ! -f "$UNIT" ]; then
	cat >"$UNIT" <<'EOF'
[Unit]
Description=BR Panel
After=network.target

[Service]
ExecStart=/usr/local/bin/brpanel -data /var/lib/brpanel -addr :8080
Restart=on-failure
# O painel precisa de root para gerenciar units systemd e o Nginx.

[Install]
WantedBy=multi-user.target
EOF
fi
systemctl daemon-reload

if [ "$UPGRADE" -eq 1 ] && systemctl is-active --quiet brpanel; then
	systemctl restart brpanel
	say "BR Panel atualizado e reiniciado (versão anterior em $BIN.bak)"
	exit 0
fi

systemctl enable --now brpanel >/dev/null 2>&1 || die "falha ao iniciar o serviço: veja journalctl -u brpanel"
say "BR Panel instalado e em execução"

# No primeiro início o painel registra o link de configuração (com o código de instalação).
LINK=""
i=0
while [ $i -lt 20 ] && [ -z "$LINK" ]; do
	LINK=$(journalctl -u brpanel --no-pager -n 50 2>/dev/null | grep -o 'http://[^ ]*/setup?code=[^ ]*' | tail -n 1 || true)
	[ -n "$LINK" ] || { sleep 1; i=$((i + 1)); }
done

echo
if [ -n "$LINK" ]; then
	echo "Abra este link para criar seu usuário e senha:"
	echo "  $LINK"
else
	echo "Veja o link de configuração com:  journalctl -u brpanel | grep 'PRIMEIRO INÍCIO'"
fi
echo "O painel escuta na porta 8080: libere-a no firewall se necessário."
