# BR Panel

O BR Panel é um painel para colocar o seu site ou sistema na internet sem precisar decorar comandos. Você aponta para o código do projeto, clica em publicar, e ele fica no ar, com endereço próprio e cadeado de segurança. Depois, quando você atualiza o código, o site se atualiza sozinho.

Ele foi pensado para quem tem um servidor (uma VPS) e quer cuidar dele sem virar especialista. Tudo acontece numa tela web, em português.

![Tela inicial do painel, com as aplicações e bancos de dados de um servidor](docs/img/09-arquitetura.png)

## O que você consegue fazer

- Publicar um projeto a partir do GitHub, sem preencher formulário comprido. O painel olha o projeto e descobre sozinho como ele funciona.
- Ter um endereço próprio (como `loja.seusite.com.br`) com cadeado de segurança, emitido e renovado sem você pedir.
- Atualizar o site a cada vez que você salva uma mudança no GitHub.
- Publicar vários projetos de um mesmo repositório, cada um como uma aplicação, e atualizar só os que mudaram.
- Publicar de novo em menos tempo: o painel guarda o que já baixou e não repete trabalho quando nada mudou.
- Acompanhar quanto de processador e de memória cada projeto usa, em gráficos das últimas 24 horas.
- Instalar programas prontos, como n8n, Uptime Kuma e Gitea, com um clique.
- Criar bancos de dados (PostgreSQL, MySQL, MongoDB e Redis) e ligá-los aos seus projetos.
- Guardar arquivos importantes e fazer cópias de segurança, manuais ou agendadas.
- Aguentar mais visitantes: o painel liga várias cópias do projeto e reparte o movimento entre elas.
- Controlar tudo também pelo celular, pelo computador ou por uma ferramenta de inteligência artificial.

Cada item está explicado, com imagens, em [Funcionalidades](docs/funcionalidades.md).

## Como funciona na prática

1. Você instala o painel no servidor com um comando, uma vez só.
2. Abre o link que aparece e cria o seu usuário e a sua senha.
3. Escolhe o projeto do GitHub que quer publicar.
4. Clica em publicar e espera. O andamento aparece na tela.

![Tela de nova aplicação, com o projeto já reconhecido pelo painel](docs/img/23-nova-app-detectada.png)

## Como instalar

Você precisa de um servidor com Ubuntu ou Debian e acesso de administrador. Cole este comando no terminal do servidor:

```sh
curl -fsSL https://raw.githubusercontent.com/viniciusdev772/br_panel/main/scripts/install.sh | sudo sh
```

No final, ele mostra um link. Abra esse link no navegador, crie o seu usuário e pronto. Para atualizar o painel mais tarde, rode o mesmo comando de novo. Os seus projetos e dados ficam como estão.

Quer ler o comando antes de executar? Ele está em [`scripts/install.sh`](scripts/install.sh).

### Para os deploys funcionarem de primeira

Um servidor novo não tem compilador, `pip`, Go, PHP nem Ruby, e projetos assim falham no primeiro build. Este comando instala o que os projetos mais comuns precisam: ferramentas de compilação e bibliotecas, Python (`pip`, `venv`, `uv`, `poetry`, `pipenv`), Go, Rust, PHP com Composer, Ruby com Bundler, Java com Maven e Bun. O Node.js o painel já instala sozinho.

```sh
curl -fsSL https://raw.githubusercontent.com/viniciusdev772/br_panel/main/scripts/install-toolchains.sh | sudo sh
```

Ele não reinicia nem altera os projetos e bancos que já rodam, e confere o SHA-256 de tudo o que baixa. Pode rodar de novo quando quiser. Veja o que ele faz em [`scripts/install-toolchains.sh`](scripts/install-toolchains.sh).

## Vários projetos no mesmo repositório

Se o seu repositório guarda um site, uma API e outras partes em pastas separadas, crie uma aplicação para cada parte e escolha a pasta dela. O painel mostra as pastas que parecem aplicações e já sugere nome e configuração. Quando você envia uma mudança ao GitHub, só as aplicações que mudaram são atualizadas.

## Publicações mais rápidas

Na primeira vez, o painel baixa o projeto e tudo o que ele usa. Nas seguintes, só chegam as mudanças do código. Os pacotes dos projetos ficam numa pasta comum a todas as aplicações, então cada um é baixado uma única vez. Se as dependências continuam as mesmas, o painel nem as instala de novo.

Em Configurações, o cartão Cache de builds mostra quanto espaço isso ocupa e deixa apagar o que ninguém usa há 30 dias. Se um projeto se comportar de um jeito estranho depois de uma atualização, o botão Refazer do zero, na página dele, reconstrói tudo. Os arquivos que a aplicação guarda em Armazenamento não são apagados.

## Um painel para o dia a dia

Cada projeto tem a sua página, com o estado atual, o histórico de publicações, os gráficos de uso e os registros do que o sistema escreveu enquanto rodava. Se algo der errado, a mensagem diz o que aconteceu e o que fazer.

![Página de um projeto, com o estado da publicação](docs/img/20-app-deploy.png)

![Gráficos de uso de processador e memória de um projeto](docs/img/22-app-metricas.png)

Dá para usar o painel no celular. As telas se ajustam ao tamanho da tela.

![O painel no celular](docs/img/25-mobile-arquitetura.png)

## Segurança

O acesso é protegido por usuário e senha, e o painel bloqueia quem erra a senha várias vezes seguidas. A senha fica guardada de um jeito que nem o próprio painel consegue ler. Dados sensíveis, como senhas de banco de dados, aparecem escondidos e só se mostram quando você pede. Para usar o painel pela internet, o ideal é abri-lo por um endereço com cadeado.

## Precisa de ajuda ou quer contribuir?

- Encontrou uma falha de segurança? Leia o [`SECURITY.md`](SECURITY.md) antes de contar a alguém.
- Dúvidas de uso: comece por [Funcionalidades](docs/funcionalidades.md) e pela [Referência técnica](docs/referencia-tecnica.md). Bugs, ideias, projetos que não sobem e sugestões de programas de um clique: abra uma issue na aba Issues e escolha o formulário que combina com o caso (lembre de remover senhas, domínios e IPs reais: o repositório é público).
- Quem programa e quer mexer no painel encontra tudo em [Documentação técnica](docs/README.md) e em [`CONTRIBUTING.md`](CONTRIBUTING.md). Não há verificação automática: rode `make check` antes de abrir o PR.
