#!/bin/sh
# Instala no servidor (Debian/Ubuntu, como root) o que os projetos mais comuns precisam para fazer deploy
# de primeira: compiladores e bibliotecas de desenvolvimento, Python (pip, venv, pipx, uv, poetry, pipenv),
# Go, Rust, PHP + Composer, Ruby + Bundler, Java + Maven e Bun. Node.js, npm, pnpm e yarn o painel já instala
# sozinho pelo nvm. Nada aqui inicia serviços nem mexe nos aplicativos e bancos que já rodam.
#
#   sudo sh scripts/install-toolchains.sh
#
# É seguro rodar de novo: o que já está instalado é pulado (Go, Rust, uv e Bun só mudam se houver versão nova
# no apt ou se você apagar o binário). Os binários baixados têm o SHA-256 conferido contra o publicado pelo
# próprio projeto antes de entrar em /usr/local.
set -eu

[ "$(id -u)" = 0 ] || { echo "erro: rode como root (sudo sh $0)" >&2; exit 1; }
command -v apt-get >/dev/null 2>&1 || { echo "erro: este script é para Debian/Ubuntu (apt)" >&2; exit 1; }

# needrestart (Ubuntu) reiniciaria serviços depois do apt: aqui ele só lista.
export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l NEEDRESTART_SUSPEND=1
case "$(uname -m)" in
x86_64) GOARCH=amd64; UVARCH=x86_64; BUNARCH=x64 ;;
aarch64 | arm64) GOARCH=arm64; UVARCH=aarch64; BUNARCH=aarch64 ;;
*) echo "erro: arquitetura $(uname -m) não suportada" >&2; exit 1 ;;
esac

step() { printf '\n==> %s\n' "$1"; }
have() { command -v "$1" >/dev/null 2>&1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# fetch URL FILE SHA256: baixa e confere o hash antes de usar.
fetch() {
	curl -fsSL --retry 3 -o "$2" "$1"
	echo "$3  $2" | sha256sum -c - >/dev/null || { echo "erro: SHA-256 de $1 não confere" >&2; exit 1; }
}

step "Pacotes do sistema (apt)"
apt-get update -qq
apt-get install -y --no-install-recommends \
	build-essential pkg-config cmake git curl wget unzip zip jq ca-certificates gnupg \
	libssl-dev libffi-dev zlib1g-dev libbz2-dev libreadline-dev libsqlite3-dev libxml2-dev libxslt1-dev \
	libjpeg-dev libpng-dev libwebp-dev libpq-dev libyaml-dev libcurl4-openssl-dev libgmp-dev \
	python3 python3-pip python3-venv python3-dev python3-setuptools python3-wheel pipx \
	php-cli php-mbstring php-xml php-curl php-zip php-mysql php-pgsql php-sqlite3 php-gd php-intl php-bcmath composer \
	ruby-full ruby-bundler \
	default-jdk-headless maven
# libmariadb-dev (cabeçalhos do cliente MySQL/MariaDB) só se o servidor já usa MariaDB: o MySQL da Oracle
# conflita com ele.
if dpkg -s mariadb-server >/dev/null 2>&1 || dpkg -s libmariadb3 >/dev/null 2>&1; then
	apt-get install -y --no-install-recommends libmariadb-dev
elif ! dpkg -s mysql-server >/dev/null 2>&1; then
	apt-get install -y --no-install-recommends libmariadb-dev
fi

step "Python: uv, poetry e pipenv"
if ! have uv; then
	base=https://github.com/astral-sh/uv/releases/latest/download
	f=uv-$UVARCH-unknown-linux-gnu.tar.gz
	curl -fsSL --retry 3 -o "$work/uv.sha" "$base/$f.sha256"
	fetch "$base/$f" "$work/$f" "$(cut -d' ' -f1 <"$work/uv.sha")"
	tar -xzf "$work/$f" -C "$work" --strip-components=1
	install -m 0755 "$work/uv" "$work/uvx" /usr/local/bin/
fi
# pipx global: cada ferramenta fica no próprio venv em /opt/pipx e o comando em /usr/local/bin.
export PIPX_HOME=/opt/pipx PIPX_BIN_DIR=/usr/local/bin
for tool in poetry pipenv; do
	have "$tool" || pipx install "$tool" >/dev/null
done
chmod -R a+rX /opt/pipx 2>/dev/null || true

step "Go"
v=$(curl -fsSL https://go.dev/VERSION?m=text | head -1)
if ! have go || [ "$(go version 2>/dev/null | cut -d' ' -f3)" != "$v" ]; then
	f=$v.linux-$GOARCH.tar.gz
	fetch "https://dl.google.com/go/$f" "$work/$f" "$(curl -fsSL "https://dl.google.com/go/$f.sha256")"
	rm -rf /usr/local/go
	tar -C /usr/local -xzf "$work/$f"
	ln -sf /usr/local/go/bin/go /usr/local/bin/go
	ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
fi

step "Rust"
if ! have cargo; then
	triple=$(uname -m)-unknown-linux-gnu
	base=https://static.rust-lang.org/rustup/dist/$triple
	curl -fsSL --retry 3 -o "$work/rustup.sha" "$base/rustup-init.sha256"
	fetch "$base/rustup-init" "$work/rustup-init" "$(cut -d' ' -f1 <"$work/rustup.sha")"
	chmod +x "$work/rustup-init"
	CARGO_HOME=/opt/rust/cargo RUSTUP_HOME=/opt/rust/rustup "$work/rustup-init" -y --profile minimal --no-modify-path --default-toolchain stable >/dev/null 2>&1
	# Os binários do toolchain, não os atalhos do rustup: os atalhos precisam de RUSTUP_HOME no ambiente
	# do build, e os apps rodam com outro HOME.
	tc=$(ls -d /opt/rust/rustup/toolchains/stable-* | head -1)
	for b in cargo rustc rustdoc; do ln -sf "$tc/bin/$b" /usr/local/bin/$b; done
	chmod -R a+rX /opt/rust
fi

step "Bun"
if ! have bun; then
	base=https://github.com/oven-sh/bun/releases/latest/download
	f=bun-linux-$BUNARCH.zip
	curl -fsSL --retry 3 -o "$work/bun.sums" "$base/SHASUMS256.txt"
	fetch "$base/$f" "$work/$f" "$(grep " $f\$" "$work/bun.sums" | cut -d' ' -f1)"
	unzip -q -o "$work/$f" -d "$work"
	install -m 0755 "$work/bun-linux-$BUNARCH/bun" /usr/local/bin/bun
fi

step "Versões instaladas"
for c in "python3 --version" "pip3 --version" "uv --version" "poetry --version" "pipenv --version" "go version" \
	"cargo --version" "rustc --version" "php --version" "composer --version" "ruby --version" "bundle --version" \
	"java -version" "mvn -v" "bun --version" "node --version" "gcc --version"; do
	printf '%-10s' "${c%% *}:"
	# shellcheck disable=SC2086
	$c 2>&1 | head -1
done
echo
echo "Pronto. Node.js, npm, pnpm e yarn o painel instala sozinho (nvm) quando um projeto precisa."
