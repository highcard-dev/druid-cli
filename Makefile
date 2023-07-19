.DEFAULT_GOAL:=help

help: ## Prints the help about targets.
	@printf "Usage:             ENV=[\033[34mprod|stage|dev\033[0m] make [\033[34mtarget\033[0m]\n"
	@printf "Default:           \033[34m%s\033[0m\n" $(.DEFAULT_GOAL)
	@printf "Targets:\n"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf " \033[34m%-17s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST) | sort

build: ## Build Daemon
	go build -o ./druid ./cmd/daemon

build-plugins: ## Build Plugins
	go build -o ./druid_rcon ./pkg/plugin/rcon/rcon.go
	go build -o ./druid_rcon_web_rust ./pkg/plugin/rcon_web_rust/rcon_web_rust.go

proto:
	protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative internal/plugin/commons/proto/kv.proto

generate-swagger:
	swag init --parseDependency -g ./cmd/server/web/server.go