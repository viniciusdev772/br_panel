---
name: brpanel-site
description: Configurar o site de uma app do BR Panel na frente do Nginx - domínio e SSL, domínios extras e redirecionamentos (www), página de manutenção, senha no site, limites de upload, várias instâncias, limite de memória/CPU e autoscale (`brpanel apps domain-add`, `maintenance`, `auth-add`, `limits`, `apps set --replicas`).
---

# Site, domínio, instâncias e escala

Pré-requisito: leu o skill `brpanel-cli`.

**Regra do Nginx:** cada mudança de site é validada com `nginx -t` antes de salvar. Se o Nginx
recusar, vem `ERRO: O Nginx recusou...` e **nada foi salvo**: corrija o valor e repita. Não há
risco de deixar o site quebrado.

**Domínio principal e extras exigem o DNS apontando para o servidor** (registro A para o IP dele).
Se não souber se está pronto, pergunte à pessoa. Sem domínio principal, as opções de Nginx abaixo
não valem (ficam salvas e entram quando houver domínio).

## Domínio principal

Na criação ou depois:

```sh
brpanel apps set loja --domain loja.example.com
brpanel apps deploy loja --wait
```

O deploy configura o Nginx e emite o certificado SSL (Let's Encrypt). Se a emissão falhar, o site
fica em HTTP e o próximo deploy tenta de novo: confira o DNS e repita o deploy.

## Domínios extras e redirecionamento (www)

Servir o mesmo site em outro endereço (ganha certificado próprio, emitido em segundo plano):

```sh
brpanel apps domain-add loja shop.example.com
```

Redirecionar um endereço para outro (padrão do `www`; `--permanent` = 301, sem ele = 302):

```sh
brpanel apps domain-add loja www.example.com --redirect-to example.com --permanent
```

Remover:

```sh
brpanel apps domain-remove loja shop.example.com
```

Até 10 endereços por app. Um domínio não pode estar em duas apps (`domínio já usado por outra
aplicação`). O que está configurado aparece em `brpanel apps show loja` (`domínio extra:`).

## Página de manutenção

Liga: o site responde 503 com a página (o app continua rodando). Desliga: volta ao normal.

```sh
brpanel apps maintenance loja on --title 'Voltamos já' --text 'Estamos atualizando o sistema.'
brpanel apps maintenance loja off
```

Sempre **desligue** ao terminar. Confira com `brpanel apps show loja` (`manutenção: não`).

## Senha para entrar no site

A senha tem de 8 a 72 caracteres e **vem da entrada padrão** (nunca na linha de comando):

```sh
printf '%s' 'senha-de-exemplo' | brpanel apps auth-add loja cliente --password-stdin
brpanel apps auth-remove loja cliente
```

Repetir o nome do usuário troca a senha. Até 10 usuários. **Não repita a senha na conversa.**
Quem pediu a senha deve ser avisado de que ela foi definida, não de qual é, a menos que ele a
tenha dado.

## Upload e tempo de espera

```sh
brpanel apps limits loja --upload-mb 100 --read-timeout 120
```

`--upload-mb` é o tamanho máximo de upload (o padrão do Nginx é só 1 MB: por isso uploads grandes
falham com 413). `--read-timeout` (10 a 3600 s, padrão 300) é quanto o Nginx espera a resposta.
`0` volta ao padrão.

## Mais instâncias, memória e CPU

```sh
brpanel apps set loja --replicas 3 --memory-mb 512 --cpu-percent 100
brpanel apps deploy loja --wait
```

- `--replicas`: de 1 a 16 cópias do app. O Nginx balanceia (**só com domínio**) e as trocas são
  uma instância por vez, sem tirar o site do ar.
- `--memory-mb` e `--cpu-percent` valem **por instância** (0 = sem limite; memória mínima 32;
  CPU mínima 5; 100 = uma CPU inteira).
- `--rate-limit REQ_S` limita requisições por segundo por visitante (0 = desligado).
- **O código precisa aguentar várias instâncias:** sem sessão, cache ou arquivos guardados só na
  memória ou no disco de um processo (use um banco ou Redis). Se não tiver certeza, pergunte à
  pessoa antes de passar de 1.

## Autoscale (sobe e desce instâncias sozinho)

```sh
brpanel apps set loja --autoscale --scale-min 1 --scale-max 4 --scale-cpu 60
brpanel apps deploy loja --wait
```

- Mantém a **CPU média** perto da meta (`--scale-cpu`, de 10 a 95). Sobe depois de 3 minutos acima
  da meta, desce só depois de 15 minutos abaixo.
- `--scale-max` é no máximo 16 e o máximo de portas fica reservado para a app.
- Meta de memória (`--scale-mem` de 10 a 95) **exige** `--memory-mb` definido.
- Desligar: `brpanel apps set loja --no-autoscale`, depois o deploy.

## Conferir

```sh
brpanel apps show loja
```

Leia: `domínio`, `url`, `instâncias`, `manutenção`, `usuários com senha`, `autoscale`. Se algo não
bate com o que você pediu, repita o comando ou abra `brpanel apps logs loja --kind deploy`.
