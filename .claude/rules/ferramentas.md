# Ferramentas de edição

- Para criar ou alterar arquivos, use as ferramentas nativas: **Write** (arquivo novo ou reescrita inteira), **Edit** (troca pontual, com `replace_all` quando preciso) e **Read**/**Grep**/**Glob** para ler e buscar.
- **Não use Python** (nem `python -c`, nem heredoc `python - <<EOF`) para editar arquivos. Scripts de substituição quebram com aspas, `\n` e fim de linha CRLF, e escondem o que mudou.
- `sed` só no máximo, para uma troca simples de uma linha que o Edit não resolve bem (por exemplo, renomear um identificador em vários arquivos). Nada de `awk`, `perl` ou heredoc com código para gerar arquivos.
- Código Go, testes e textos novos entram com Write. Depois de editar Go, rode `gofmt -w` nos arquivos tocados.
- Antes de usar Edit num arquivo, leia-o com Read na mesma conversa: o Edit falha se o trecho não bater exatamente, e o erro mostra o que ajustar.
