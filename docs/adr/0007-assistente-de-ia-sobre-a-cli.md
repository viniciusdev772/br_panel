# ADR 0007 — Assistente de IA que usa a CLI como ferramentas

- **Status:** aceita
- **Contexto:** dar ao painel um agente de IA (OpenRouter e similares) que opere o servidor, sem criar uma segunda forma de mexer no sistema.
- **Decisão:**
  - `internal/ai` implementa o laço do agente (pede ao modelo, executa as ferramentas pedidas, devolve os resultados) com `net/http` e a biblioteca padrão, sem framework: Eino, LangChainGo e Genkit resolvem grafos e RAG que não usamos, e o SDK oficial do OpenRouter está em beta.
  - Três adaptadores falam os formatos Chat Completions, Responses e Messages, sempre com streaming. O Responses do OpenRouter é sem estado (rejeita `store` e `previous_response_id`), então o histórico inteiro é reenviado nos três.
  - As ferramentas são geradas do `cli.Catalog` e executadas dentro do processo pelas rotas da CLI (`cliHandler`), com a mesma validação. Cada comando é classificado como livre, com confirmação ou retido (segredos, login, atualização); um teste falha se um comando novo ficar sem classificação.
  - O navegador guarda a conversa e o servidor não tem estado. Uma chamada que altera o servidor para a conversa até o usuário aprovar; só o servidor acrescenta `--yes`, depois da aprovação.
  - Configuração (formato, endereço, modelo, chave) no SQLite, como o resto; nada de variável de ambiente.
- **Consequências:** + uma única superfície de ação, já validada; + serve a qualquer provedor compatível. − o texto da conversa (inclusive logs lidos) vai a um terceiro escolhido pelo dono; − modelos sem suporte a ferramentas não funcionam; − o histórico some ao recarregar a página.
