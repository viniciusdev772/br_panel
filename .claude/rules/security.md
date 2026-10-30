---
paths:
  - "internal/**"
  - "cmd/**"
---

# Segurança (regras do projeto)

O painel roda como root e executa código vindo de repositórios Git: trate cada entrada como hostil.

1. **Validar antes de gravar.** Tudo que chega a um comando, unit systemd ou arquivo Nginx passa por `internal/validate` ou `settings.Validate` primeiro (nome, repositório, branch, domínio, caminhos, e-mail, env).
2. **Sem shell por concatenação.** Programas externos recebem argumentos separados (`exec.Command(name, args...)`). A única exceção é o `sh -c` dos comandos de build/início definidos pelo admin — nunca monte esses comandos a partir de dados de terceiros sem `safeRel`/quoting (veja `detect`).
3. **Segredos.** Não registre em log nem em argv: senha, token de instalação, chave `.pem`, segredo do webhook. O token do GitHub vai **só ao git**, via `GIT_CONFIG_*` (`githubapp.AuthEnv`); nunca ao ambiente do build ou da aplicação.
4. **Comparações de segredo em tempo constante** (`auth.ConstEqual`, `hmac.Equal`). Hash de senha: Argon2id (`settings.HashPassword`).
5. **POST autenticado exige CSRF** (`protected()`); só o webhook fica fora, e ele exige HMAC válido.
6. **Cabeçalhos `X-Real-IP`/`X-Forwarded-Proto`** só valem vindos de loopback (`auth.ClientIP`, `auth.IsHTTPS`).
7. Permissões de arquivo: banco 0600, env files 0600, diretório de dados 0750.
8. Qualquer nova rota que altera estado: método POST, `protected()`, teste cobrindo sem-sessão e sem-CSRF.

Em dúvida, rode `/security-review` ou peça ao agente `security-reviewer`.
