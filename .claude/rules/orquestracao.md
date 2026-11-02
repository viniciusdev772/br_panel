# Orquestração com o Maestri

Vale quando o Claude Code trabalha como Maestro, delegando pacotes de trabalho a outros agentes.

- **Quem faz o quê.** Quem implementa é o **OpenCode** (`maestri recruit "Nome" --preset "OpenCode" --role "Implementador br_panel"`). O Claude Code só planeja, delega, verifica, publica e atualiza a VPS. Não recrute mais agentes Claude Code (nem Codex) para implementar; um que já esteja trabalhando termina o pacote atual e é dispensado.
- **Um agente por vez, no mesmo checkout.** Os pacotes mexem nos mesmos arquivos (migrações do banco, rotas, templates); em paralelo ou em worktrees isso só gera conflito. Só recrute o próximo depois de verificar e dar o push do anterior. Dispense (`maestri dismiss`) o agente que terminou.
- **Plano compartilhado.** A nota `plano-brpanel` guarda o acordo de trabalho, a ordem dos pacotes e a tabela de status. Conecte a nota a cada recrutado (`maestri connect "plano-brpanel" "Nome"`) e peça que atualize a própria linha. A senha da VPS (nota `vps`) nunca vai para outro agente.
- **Pedido e retorno.** Envie a tarefa com `maestri ask` em segundo plano e peça o retorno em `maestri ask "Claude Code" "<resumo>"`. Não repita o pedido nem edite arquivos enquanto o agente trabalha.
- **Commit sem push.** O agente faz o commit; o Maestro roda a suíte no Linux, dá o push e atualiza a VPS. Se algo falhar, devolve ao mesmo agente com o erro exato.
- **Revisão.** Antes do push, o OpenCode já conectado revisa o diff do pacote (`maestri ask "OpenCode" "revise o último commit..."`) e o Maestro confere o que ele apontar.
- **Verificação mínima por pacote:** `gofmt -l .`, `go vet` para Linux, testes no Linux (na VPS, com Go temporário em `/tmp`, apagado no fim) e, quando o pacote mexe em systemd ou Nginx, um teste real descartável na VPS. Tudo que for criado lá é removido depois.
- **Honestidade no relatório.** Diga o que foi testado de verdade e o que não foi; não marque um pacote como pronto com teste falhando.
