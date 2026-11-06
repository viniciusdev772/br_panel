#!/bin/sh
# Confere, em português, se a CLI do BR Panel está pronta para uso. Só lê: não altera nada.
#   sh verificar.sh
# Saída: linhas "OK:" (pode seguir), "ERRO:" (corrija) e "DICA:" (o que fazer).
# Código de saída: 0 pronto, 1 falta algo.

if ! command -v brpanel >/dev/null 2>&1; then
	echo "ERRO: o programa brpanel não está instalado neste computador."
	echo "DICA: no servidor do painel ele já existe (veja /usr/local/bin/brpanel)."
	echo "DICA: em outro computador, instale só a CLI: curl -fsSL https://raw.githubusercontent.com/viniciusdev772/br_panel/main/scripts/install-cli.sh | sh"
	exit 1
fi
echo "OK: brpanel encontrado em $(command -v brpanel)"

echo "--- brpanel whoami ---"
brpanel whoami

# No servidor sem login remoto, o socket só abre para o root.
if [ "$(uname -s)" = "Linux" ] && [ "$(id -u)" != "0" ] && ! brpanel whoami 2>/dev/null | grep -q "modo: remoto"; then
	echo "DICA: você não é root e não há login remoto salvo. No servidor rode: sudo sh $0"
fi

echo "--- brpanel status ---"
if out=$(brpanel status 2>&1); then
	echo "$out"
	echo "OK: a CLI está falando com o painel."
	exit 0
fi
echo "$out"
echo "ERRO: não consegui ler o estado do painel. Siga as linhas DICA acima."
exit 1
