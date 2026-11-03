package templates

// catalog is the list of one-click apps. To add one: pick the cheapest source (an npm package or a
// Git tag; a release binary only with a published SHA-256), keep Build and Start to one line each
// (longer logic goes into a script in Files), persist data in a Mount, and run it on a real server
// before listing it. Start must listen on $PORT and $HOST.
var catalog = []Template{
	{
		ID: "n8n", Name: "n8n", Category: "Automação", Icon: "account_tree",
		Summary:     "Automação de fluxos de trabalho com editor visual (alternativa ao Zapier).",
		Description: "Instala o n8n a partir do pacote npm oficial e o roda como serviço. Os fluxos, credenciais e o banco SQLite ficam numa pasta de armazenamento que sobrevive a deploys. Um novo deploy atualiza para a versão escolhida (por padrão, a mais recente).",
		Docs:        "https://docs.n8n.io/",
		After:       "Abra o endereço da aplicação e crie a conta de administrador: o primeiro acesso define o dono. Com domínio e SSL os webhooks já saem com o endereço certo.",
		Version:     "latest", VersionHint: "latest ou um número, como 2.45.7",
		Files: map[string]string{
			"package.json": "{\n  \"name\": \"n8n-instance\",\n  \"private\": true,\n  \"version\": \"1.0.0\"\n}\n",
		},
		Requires: []string{"node", "npm"},
		Build:    `npm install --no-audit --no-fund --no-progress "n8n@$TPL_VERSION"`,
		Start:    `N8N_PORT=$PORT N8N_LISTEN_ADDRESS=$HOST N8N_USER_FOLDER=$VOLUME_DATA ./node_modules/.bin/n8n start`,
		Env: []EnvVar{
			{Key: "N8N_ENCRYPTION_KEY", Secret: true},
			{Key: "N8N_DIAGNOSTICS_ENABLED", Value: "false"},
			{Key: "GENERIC_TIMEZONE", Value: "America/Sao_Paulo"},
			{Key: "N8N_PROXY_HOPS", Value: "1", When: "domain"},
			{Key: "N8N_EDITOR_BASE_URL", Value: "https://{{domain}}/", When: "domain"},
			{Key: "WEBHOOK_URL", Value: "https://{{domain}}/", When: "domain"},
			{Key: "N8N_SECURE_COOKIE", Value: "false", When: "nodomain"}, // plain http by IP:port has no HTTPS for the cookie
		},
		Mounts:   []Mount{{Name: "data", Target: "data"}},
		MemoryMB: 1024,
	},
	{
		ID: "uptime-kuma", Name: "Uptime Kuma", Category: "Monitoramento", Icon: "monitor_heart",
		Summary:     "Monitoramento de sites e serviços com alertas e página de status.",
		Description: "Baixa a versão escolhida do repositório oficial, instala as dependências e roda o servidor. O banco e as configurações ficam numa pasta de armazenamento que sobrevive a deploys.",
		Docs:        "https://github.com/louislam/uptime-kuma/wiki",
		After:       "Abra o endereço da aplicação e crie o usuário administrador no primeiro acesso.",
		Repo:        "https://github.com/louislam/uptime-kuma.git",
		Version:     "2.5.9", VersionHint: "uma tag do repositório, como 2.5.9",
		Requires: []string{"node", "npm", "git"},
		Build:    `npm ci --omit dev --no-audit --no-fund && npm run download-dist`,
		Start:    `UPTIME_KUMA_PORT=$PORT UPTIME_KUMA_HOST=$HOST DATA_DIR=$VOLUME_DATA/ node server/server.js`,
		Mounts:   []Mount{{Name: "data", Target: "data"}},
		MemoryMB: 512,
	},
	{
		ID: "gitea", Name: "Gitea", Category: "Desenvolvimento", Icon: "merge_type",
		Summary:     "Hospedagem de repositórios Git própria, leve e com interface parecida com a do GitHub.",
		Description: "Baixa o binário oficial do Gitea (com a soma SHA-256 publicada, conferida antes de usar), usa SQLite e guarda repositórios e configuração numa pasta de armazenamento. O acesso por SSH do Gitea fica desligado: clone por HTTPS.",
		Docs:        "https://docs.gitea.com/",
		After:       "Abra o endereço da aplicação e conclua a instalação inicial (o banco SQLite já vem escolhido); crie a conta de administrador nessa tela.",
		Version:     "28.3.0", VersionHint: "versão do Gitea, como 28.3.0",
		Files:    map[string]string{"install.sh": giteaInstall, "run.sh": giteaRun},
		Requires: []string{"curl", "git"},
		Build:    `sh install.sh`,
		Start:    `sh run.sh`,
		Env: []EnvVar{
			{Key: "GITEA__server__DISABLE_SSH", Value: "true"},
			{Key: "GITEA__database__DB_TYPE", Value: "sqlite3"},
			{Key: "GITEA__server__DOMAIN", Value: "{{domain}}", When: "domain"},
			{Key: "GITEA__server__ROOT_URL", Value: "https://{{domain}}/", When: "domain"},
			{Key: "GITEA__security__SECRET_KEY", Secret: true},
			{Key: "GITEA__service__DISABLE_REGISTRATION", Value: "true"},
		},
		Mounts:   []Mount{{Name: "data", Target: "data"}},
		MemoryMB: 512,
	},
}

// giteaInstall downloads the release binary and refuses it unless it matches the published SHA-256.
const giteaInstall = `set -eu
case "$(uname -m)" in
x86_64 | amd64) A=amd64 ;;
aarch64 | arm64) A=arm64 ;;
*) echo "arquitetura não suportada: $(uname -m)" >&2; exit 1 ;;
esac
V="$TPL_VERSION"
URL="https://dl.gitea.com/gitea/$V/gitea-$V-linux-$A"
curl -fsSL -o gitea.new "$URL"
curl -fsSL -o gitea.sha256 "$URL.sha256"
echo "$(cut -d' ' -f1 gitea.sha256)  gitea.new" | sha256sum -c -
chmod 755 gitea.new
mv -f gitea.new gitea
`

// giteaRun writes the settings from the GITEA__ variables into app.ini (the plain binary does not
// read them by itself) and then starts the server in the foreground.
const giteaRun = `set -eu
export GITEA_WORK_DIR="$VOLUME_DATA"
export GITEA__server__HTTP_PORT="$PORT"
export GITEA__server__HTTP_ADDR="$HOST"
export GITEA__server__APP_DATA_PATH="$VOLUME_DATA/data"
mkdir -p "$VOLUME_DATA/custom/conf"
CONF="$VOLUME_DATA/custom/conf/app.ini"
./gitea environment-to-ini --config "$CONF"
exec ./gitea web --config "$CONF"
`
