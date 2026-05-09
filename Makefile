.PHONY: test build build-coldstarter-image

VERSION ?= "dev"
COLDSTARTER_IMAGE ?= druid-coldstarter:local

generate-api: ## Generate API types from OpenAPI spec
	@echo "Generating API types from OpenAPI spec..."
	@which oapi-codegen > /dev/null || (echo "Installing oapi-codegen..." && go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.1)
	@PATH="$(shell go env GOPATH)/bin:$$PATH" oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml

validate-api: ## Validate OpenAPI spec
	@echo "Validating OpenAPI spec..."
	@which oapi-codegen > /dev/null || (echo "Installing oapi-codegen..." && go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.1)
	@PATH="$(shell go env GOPATH)/bin:$$PATH" oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml > /dev/null
	@echo "✓ OpenAPI spec is valid"

build: generate-api ## Build Daemon and helper binaries
	CGO_ENABLED=0 go build -ldflags "-X github.com/highcard-dev/daemon/internal.Version=$(VERSION)" -o ./bin/druid ./apps/druid
	CGO_ENABLED=0 go build -ldflags "-X github.com/highcard-dev/daemon/internal.Version=$(VERSION)" -o ./bin/druid-client ./apps/druid-client
	CGO_ENABLED=0 go build -ldflags "-X github.com/highcard-dev/daemon/internal.Version=$(VERSION)" -o ./bin/druid-coldstarter ./apps/druid-coldstarter

build-coldstarter-image: ## Build local druid-coldstarter Docker image without pushing
	VERSION=$(VERSION) IMAGE=$(COLDSTARTER_IMAGE) ./scripts/build_coldstarter_image.sh

build-x86-docker:
	docker run -e GOOS=linux -e GOARCH=amd64 -it --rm -v ./:/app -w /app --entrypoint=/bin/bash docker.elastic.co/beats-dev/golang-crossbuild:1.22.5-main  -c 'CGO_ENABLED=1 go build -ldflags "-X github.com/highcard-dev/daemon/internal.Version=$(VERSION)" -o ./bin/x86/druid'

install: ## Install Daemon
	cp ./bin/druid /usr/local/bin/druid

generate-md-docs:
	go run ./docs_md/main.go

run: ## Run Daemon
	go run ./apps/druid

mock:
	mockgen -source=internal/core/ports/services_ports.go -destination test/mock/services.go

test:
	go test -v ./...

test-clean:
	go clean -testcache
	go test -v ./test

test-docker:
	docker build . -f Dockerfile.testing -t druid-cli-test
	docker run -v ./:/app --entrypoint=/bin/bash --rm druid-cli-test -c "go test -v ./..."


test-integration:
	go test -timeout 1200s -tags=integration ./test/integration

test-integration-docker:
	docker build . -f Dockerfile.testing -t druid-cli-test
	docker run -v ./:/app --entrypoint=/bin/bash --rm druid-cli-test -c "go test -timeout 1200s -tags=integration -v ./test/integration"
	docker run -v ./:/app --entrypoint=/bin/bash --rm druid-cli-test -c "go test -timeout 1200s -tags=integration -v ./test/integration/commands"

test-integration-docker-debug:
	docker build . -f Dockerfile.testing -t druid-cli-test
	docker run -v ./:/app --entrypoint=/bin/bash --rm -p 2345:2345 -it druid-cli-test -c "dlv --listen=:2345 --headless=true --log=true --log-output=debugger,debuglineerr,gdbwire,lldbout,rpc --accept-multiclient --api-version=2 test ./test/integration/commands"
