# ADR 0001 — Serviços systemd em vez de contêineres

- **Status:** aceita
- **Contexto:** o objetivo é uma alternativa ao EasyPanel para quem usa mais serviços nativos do Linux do que Docker. Contêineres exigem daemon, imagens e rede próprios, e consomem recursos em VPS pequenas.
- **Decisão:** cada aplicação vira uma unit `brpanel-<app>.service` (usuário sem privilégios, `NoNewPrivileges`, `PrivateTmp`, `ProtectSystem=full`, contabilização de CPU/memória). Logs vêm do journal; métricas, do cgroup do serviço.
- **Consequências:** + leve, usa ferramentas que o administrador já conhece (`systemctl`, `journalctl`); + monitoramento nativo via cgroup v2. − isolamento mais fraco que contêineres; − dependências do projeto (Node, Python, Go…) precisam existir no servidor; − o deploy não é atômico.
