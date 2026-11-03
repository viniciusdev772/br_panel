#!/bin/sh
# Instala a CLI do BR Panel no SEU computador (Linux ou macOS) para controlar um painel remoto.
#
#   curl -fsSL https://raw.githubusercontent.com/viniciusdev772/br_panel/main/scripts/install-cli.sh | sh
#   ... | sh -s -- --version v0.2.0      # uma versão específica em vez da última
#   ... | sh -s -- --dir "$HOME/bin"     # outra pasta de instalação
#
# Depois: brpanel login ENDEREÇO-DO-PAINEL   (o token se cria na aba CLI do painel).
# No Windows use scripts/install-cli.ps1. No servidor do painel não precisa: o binário
# brpanel dele já inclui a CLI.
set -eu

REPO="viniciusdev772/br_panel"
VERSION=latest
DIR=""
BASE=""
FORCE=0

die() { echo "erro: $*" >&2; exit 1; }
say() { echo "==> $*"; }

while [ $# -gt 0 ]; do
	case "$1" in
	--version) [ $# -ge 2 ] || die "--version precisa de um valor (ex.: v0.2.0)"; VERSION=$2; shift 2 ;;
	--dir) [ $# -ge 2 ] || die "--dir precisa de uma pasta"; DIR=$2; shift 2 ;;
	--base-url) [ $# -ge 2 ] || die "--base-url precisa de uma URL"; BASE=$2; shift 2 ;; # espelho dos arquivos da release
	--force) FORCE=1; shift ;;
	-h | --help) sed -n '2,9p' "$0" 2>/dev/null || true; exit 0 ;;
	*) die "opção desconhecida: $1" ;;
	esac
done

TERMUX=0
case "$(uname -s)" in
Linux)
	OS=linux
	# Termux (Android) recusa binários Linux estáticos ("unexpected e_type: 2"): usa o build android (PIE).
	if [ "$(uname -o 2>/dev/null)" = Android ] || [ -n "${TERMUX_VERSION:-}" ] || [ -d /data/data/com.termux ]; then
		OS=android
		TERMUX=1
	fi
	;;
Darwin) OS=darwin ;;
*) die "sistema não suportado: $(uname -s). No Windows use: irm https://raw.githubusercontent.com/$REPO/main/scripts/install-cli.ps1 | iex" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) ARCH=amd64 ;;
aarch64 | arm64 | armv8l) ARCH=arm64 ;;
*) die "arquitetura não suportada: $(uname -m) (use amd64 ou arm64)" ;;
esac

if [ "$FORCE" -eq 0 ] && [ -f /etc/systemd/system/brpanel.service ]; then
	say "este computador já é um servidor do BR Panel: o comando 'brpanel' dele já inclui a CLI (use: sudo brpanel apps list)."
	say "para instalar a CLI independente mesmo assim, rode de novo com --force."
	exit 0
fi

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO "$2" "$1"; }
else
	die "precisa de curl ou wget"
fi

if [ "$OS" = android ] && [ "$ARCH" != arm64 ]; then
	die "no Termux só há build para arm64 (aarch64); este aparelho é $(uname -m)"
fi

if [ -z "$DIR" ]; then
	if [ "$TERMUX" -eq 1 ] && [ -n "${PREFIX:-}" ]; then DIR="$PREFIX/bin" # já está no PATH do Termux
	elif [ "$(id -u)" -eq 0 ]; then DIR=/usr/local/bin; else DIR="$HOME/.local/bin"; fi
fi
if [ -z "$BASE" ]; then
	if [ "$VERSION" = latest ]; then
		BASE="https://github.com/$REPO/releases/latest/download"
	else
		BASE="https://github.com/$REPO/releases/download/$VERSION"
	fi
fi

NAME="brpanel-cli-$OS-$ARCH"
TMP=$(mktemp -d) || die "não consegui criar uma pasta temporária"
trap 'rm -rf "$TMP"' EXIT INT TERM

say "baixando $NAME ($VERSION)"
fetch "$BASE/$NAME" "$TMP/$NAME" || die "não consegui baixar $BASE/$NAME (a release existe e tem este arquivo?)"
fetch "$BASE/$NAME.sha256" "$TMP/$NAME.sha256" || die "não consegui baixar a soma SHA-256"

WANT=$(cut -d' ' -f1 "$TMP/$NAME.sha256" | tr -d '\r\n ')
if command -v sha256sum >/dev/null 2>&1; then
	GOT=$(sha256sum "$TMP/$NAME" | cut -d' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
	GOT=$(shasum -a 256 "$TMP/$NAME" | cut -d' ' -f1)
else
	die "precisa de sha256sum ou shasum para conferir o download"
fi
[ -n "$WANT" ] && [ "$WANT" = "$GOT" ] || die "a soma SHA-256 não confere: download corrompido ou adulterado; nada foi instalado"

mkdir -p "$DIR" || die "não consegui criar $DIR"
chmod 0755 "$TMP/$NAME"
mv -f "$TMP/$NAME" "$DIR/brpanel" || die "não consegui gravar em $DIR (tente --dir PASTA ou rode com sudo)"
say "instalado em $DIR/brpanel"

# Garante o PATH nos próximos terminais: acrescenta (uma vez só) ao ~/.bashrc e ao ~/.profile.
PATHLINE="export PATH=\"$DIR:\$PATH\" # brpanel"
add_path() {
	[ -f "$1" ] && grep -qF "# brpanel" "$1" 2>/dev/null && return 0
	if [ -f "$1" ] || [ "$1" = "$HOME/.bashrc" ] || [ "$1" = "$HOME/.profile" ]; then
		printf '\n%s\n' "$PATHLINE" >>"$1" 2>/dev/null && say "PATH ajustado em $1"
	fi
}
case ":$PATH:" in
*":$DIR:"*) ;;
*)
	add_path "$HOME/.bashrc"
	add_path "$HOME/.profile"
	[ -f "$HOME/.zshrc" ] && add_path "$HOME/.zshrc"
	say "abra um novo terminal ou rode:  export PATH=\"$DIR:\$PATH\""
	;;
esac
echo
echo "Pronto. Próximos passos:"
echo "  1. No painel, aba CLI > Acesso remoto: crie um token."
echo "  2. brpanel login ENDEREÇO-DO-PAINEL"
echo "  3. brpanel status"
echo "Dúvidas: brpanel help   |   guia para agentes de IA: brpanel guide"
