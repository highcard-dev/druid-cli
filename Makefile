.DEFAULT_GOAL:=help

VERSION ?= "dev"

help: ## Prints the help about targets.
	@printf "Usage:             ENV=[\033[34mprod|stage|dev\033[0m] make [\033[34mtarget\033[0m]\n"
	@printf "Default:           \033[34m%s\033[0m\n" $(.DEFAULT_GOAL)
	@printf "Targets:\n"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf " \033[34m%-17s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

build: ## Build Daemon
	CGO_ENABLED=0 go build -ldflags "-X github.com/highcard-dev/daemon/cmd.version=$(VERSION)" -o ./bin/druid

install: ## Install Daemon
	cp ./bin/druid /usr/local/bin/druid

build-plugins: ## Build Plugins
	CGO_ENABLED=0 go build -o ./bin/druid_rcon ./plugin/rcon/rcon.go
	CGO_ENABLED=0 go build -o ./bin/druid_rcon_web_rust ./plugin/rcon_web_rust/rcon_web_rust.go

proto:
	protoc --go_out=paths=source_relative:./ --go-grpc_out=paths=source_relative:./ --go-grpc_opt=paths=source_relative plugin/proto/*.proto

generate-swagger:
	swag init -g ./cmd/server/web/server.go --overridesFile override.swag

run: ## Run Daemon
	go run main.go