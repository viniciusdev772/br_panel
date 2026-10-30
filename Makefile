# Common development tasks. `make help` lists them.
GO      ?= go
BIN     := bin/brpanel
PKG     := ./...
DATA    ?= ./data
ADDR    ?= 127.0.0.1:8080
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.DEFAULT_GOAL := help
.PHONY: help build run test race cover vet fmt fmt-check check tidy vuln clean screenshots

help: ## Lista os alvos
	@awk 'BEGIN{FS=":.*## "} /^[a-z-]+:.*## /{printf "  \033[36m%-12s\033[0m %s\n",$$1,$$2}' $(MAKEFILE_LIST)

build: ## Compila para bin/brpanel (sempre com -o: nunca deixe binários na raiz)
	$(GO) build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/brpanel

run: build ## Executa localmente (DATA=./data ADDR=127.0.0.1:8080)
	$(BIN) -data $(DATA) -addr $(ADDR)

test: ## Testes
	$(GO) test $(PKG)

race: ## Testes com detector de corrida
	$(GO) test -race $(PKG)

cover: ## Cobertura (gera coverage.out e resume por pacote)
	$(GO) test -coverprofile=coverage.out ./internal/...
	$(GO) tool cover -func=coverage.out | tail -1

vet: ## go vet
	$(GO) vet $(PKG)

fmt: ## Formata o código
	gofmt -w cmd internal web tools

fmt-check: ## Falha se algo não estiver formatado
	@test -z "$$(gofmt -l cmd internal web tools)" || { echo "arquivos fora do padrão:"; gofmt -l cmd internal web tools; exit 1; }

check: fmt-check vet race ## Tudo que o CI roda

tidy: ## go mod tidy
	$(GO) mod tidy

vuln: ## Vulnerabilidades conhecidas nas dependências (requer rede)
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest $(PKG)

screenshots: build ## Capturas de todas as telas (veja scripts/screenshots/README.md)
	scripts/screenshots/run.sh /tmp/brpanel-shots

clean: ## Remove artefatos de build
	rm -rf bin coverage.out
