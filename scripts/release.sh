#!/bin/sh
# Publica uma release à mão (sem GitHub Actions): compila os 9 binários, gera os .sha256 e cria a release.
#
#   scripts/release.sh v0.3.1
#
# Precisa de Go, git e gh (gh auth login). Rode na main atualizada, com o CHANGELOG já ajustado.
set -eu

TAG=${1:-}
case "$TAG" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo "uso: scripts/release.sh vX.Y.Z" >&2; exit 2 ;; esac
cd "$(dirname "$0")/.."

[ "$(git rev-parse --abbrev-ref HEAD)" = main ] || { echo "erro: rode na branch main" >&2; exit 1; }
git diff --quiet && git diff --cached --quiet || { echo "erro: há mudanças não commitadas" >&2; exit 1; }
VER=${TAG#v}
sed -n "/^## \[$VER\]/,/^## \[/p" CHANGELOG.md | sed '1d;$d' >dist-notes.md.tmp
[ -s dist-notes.md.tmp ] || { rm -f dist-notes.md.tmp; echo "erro: sem seção [$VER] no CHANGELOG.md" >&2; exit 1; }

rm -rf dist && mkdir dist
for arch in amd64 arm64; do
	CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags "-s -w -X github.com/viniciusdev772/br_panel/internal/cli.Version=$TAG" -o dist/brpanel-linux-$arch ./cmd/brpanel
	for os in linux darwin windows; do
		ext=""; [ "$os" = windows ] && ext=.exe
		CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X github.com/viniciusdev772/br_panel/internal/cli.Version=$TAG" -o dist/brpanel-cli-$os-$arch$ext ./cmd/brpanel-cli
	done
done
# Termux (Android) só executa binários PIE; GOOS=android gera um sem cgo.
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath -ldflags "-s -w -X github.com/viniciusdev772/br_panel/internal/cli.Version=$TAG" -o dist/brpanel-cli-android-arm64 ./cmd/brpanel-cli
(cd dist && for f in brpanel-*; do sha256sum "$f" >"$f.sha256"; done)

git tag -a "$TAG" -m "$TAG"
git push origin "$TAG"
gh release create "$TAG" dist/* --title "$TAG" --notes-file dist-notes.md.tmp
rm -f dist-notes.md.tmp
echo "Release $TAG publicada com $(ls dist | wc -l) arquivos (dist/ fica fora do Git)."
