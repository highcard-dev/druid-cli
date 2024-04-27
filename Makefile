.PHONY: test

VERSION ?= "dev"

build: ## Build Daemon
	CGO_ENABLED=0 go build -ldflags "-X github.com/highcard-dev/daemon/internal.Version=$(VERSION)" -o ./bin/druid

install: ## Install Daemon
	cp ./bin/druid /usr/local/bin/druid

build-plugins: ## Build Plugins
	CGO_ENABLED=0 go build -o ./bin/druid_rcon ./plugin/rcon/rcon.go
	CGO_ENABLED=0 go build -o ./bin/druid_rcon_web_rust ./plugin/rcon_web_rust/rcon_web_rust.go

proto:
	protoc --go_out=paths=source_relative:./ --go-grpc_out=paths=source_relative:./ --go-grpc_opt=paths=source_relative plugin/proto/*.proto

generate-swagger:
	swag init -g ./cmd/server/web/server.go --overridesFile override.swag

generate-md-docs:
	go run ./docs_md/main.go

run: ## Run Daemon
	go run main.go

mock:
	mockgen -source=internal/core/ports/services_ports.go -destination test/mock/services.go

test:
	go test -v ./test

test-integration:
	go test -v ./test/integration

test-integration-docker:
	docker build . -f Dockerfile.testing -t druid-cli-test
	docker run --rm druid-cli-test bash -c "go test -v ./test/integration"
