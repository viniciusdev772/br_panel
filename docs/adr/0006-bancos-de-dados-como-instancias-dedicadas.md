# ADR 0006 — Bancos de dados como instâncias dedicadas (systemd), com variáveis de referência

- **Status:** aceita
- **Contexto:** plataformas como o Railway tratam um banco como um serviço do projeto: um clique cria o banco, mostra a conexão e liga
  as aplicações por variáveis de referência. Queremos a mesma experiência sem Docker.
- **Decisão:** cada banco é uma instância própria (`brpaneldb-<nome>.service`) iniciada com os binários do servidor, com diretório de
  dados, porta e credenciais independentes, ouvindo em `127.0.0.1`. Alternativa descartada: usar o servidor único do sistema e
  criar apenas "databases" lógicos — não daria métricas, parada, backup e exclusão por recurso. As aplicações referenciam
  `${{nome.VARIAVEL}}`, resolvido no deploy. O painel instala os pacotes com apt quando possível. Os motores ficam atrás de uma interface
  (`dbengine.engine`), então um novo motor é um arquivo.
- **Consequências:** + criar/parar/medir/fazer backup de um banco é igual a qualquer recurso; + trocar a senha não exige editar apps.
  − uma instância por banco consome mais RAM que bancos lógicos num servidor único; − todos os bancos compartilham o usuário do SO;
  − MongoDB exige instalação manual (fora dos repositórios padrão); − sem alta disponibilidade nem pooling.
