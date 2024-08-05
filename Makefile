.PHONY: test

VERSION ?= "dev"

build: ## Build Daemon
	CGO_ENABLED=1 go build -ldflags "-X github.com/highcard-dev/daemon/internal.Version=$(VERSION)" -o ./bin/druid

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

test_clean:
	go clean -testcache
	go test -v ./test

test-integration:
	go test -v ./test/integration

test-integration-docker:
	docker build . -f Dockerfile.testing -t druid-cli-test
	docker run -v ./:/app --entrypoint=/bin/bash --rm druid-cli-test -c "go test -v ./test/integration"
	docker run -v ./:/app --entrypoint=/bin/bash --rm druid-cli-test -c "go test -v ./test/integration/commands"

test-integration-docker-debug:
	docker build . -f Dockerfile.testing -t druid-cli-test
	docker run -v ./:/app --entrypoint=/bin/bash --rm -p 2345:2345 -it druid-cli-test -c "dlv --listen=:2345 --headless=true --log=true --log-output=debugger,debuglineerr,gdbwire,lldbout,rpc --accept-multiclient --api-version=2 test ./test/integration/commands"