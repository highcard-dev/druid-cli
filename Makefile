.PHONY: test build k3d-build-pull-image test-integration test-integration-docker test-integration-kubernetes kind-integration-up kind-integration-down

VERSION ?= "dev"
DRUID_K8S_PULL_IMAGE ?= druid:local
K3D_CLUSTER ?= druid-gs
INTEGRATION_TIMEOUT ?= 1200s
KIND_CLUSTER ?= druid-cli-integration
KIND_VERSION ?= v0.27.0
GO_BIN ?= $(shell go env GOPATH)/bin

generate-api: ## Generate API types from OpenAPI spec
	@echo "Generating API types from OpenAPI spec..."
	@which oapi-codegen > /dev/null || (echo "Installing oapi-codegen..." && go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.1)
	@PATH="$(shell go env GOPATH)/bin:$$PATH" oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	@PATH="$(shell go env GOPATH)/bin:$$PATH" oapi-codegen -config api/dev-oapi-codegen.yaml api/dev.openapi.yaml
	@PATH="$(shell go env GOPATH)/bin:$$PATH" oapi-codegen -config api/callback-oapi-codegen.yaml api/callback.openapi.yaml

validate-api: ## Validate OpenAPI spec
	@echo "Validating OpenAPI spec..."
	@which oapi-codegen > /dev/null || (echo "Installing oapi-codegen..." && go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.1)
	@PATH="$(shell go env GOPATH)/bin:$$PATH" oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml > /dev/null
	@PATH="$(shell go env GOPATH)/bin:$$PATH" oapi-codegen -config api/dev-oapi-codegen.yaml api/dev.openapi.yaml > /dev/null
	@PATH="$(shell go env GOPATH)/bin:$$PATH" oapi-codegen -config api/callback-oapi-codegen.yaml api/callback.openapi.yaml > /dev/null
	@echo "✓ OpenAPI spec is valid"

build: generate-api ## Build Druid and helper binaries
	CGO_ENABLED=0 go build -ldflags "-X github.com/highcard-dev/daemon/internal.Version=$(VERSION)" -o ./bin/druid ./apps/druid
	CGO_ENABLED=0 go build -ldflags "-X github.com/highcard-dev/daemon/internal.Version=$(VERSION)" -o ./bin/druid-coldstarter ./apps/druid-coldstarter

k3d-build-pull-image: ## Build the unified Druid runtime image and import it into local k3d.
	docker build . -f Dockerfile --build-arg "VERSION=$(VERSION)" -t "$(DRUID_K8S_PULL_IMAGE)"
	@docker rm -f "k3d-$(K3D_CLUSTER)-tools" >/dev/null 2>&1 || true
	k3d image import "$(DRUID_K8S_PULL_IMAGE)" -c "$(K3D_CLUSTER)"

build-x86-docker:
	docker run -e GOOS=linux -e GOARCH=amd64 -it --rm -v ./:/app -w /app --entrypoint=/bin/bash docker.elastic.co/beats-dev/golang-crossbuild:1.22.5-main  -c 'CGO_ENABLED=1 go build -ldflags "-X github.com/highcard-dev/daemon/internal.Version=$(VERSION)" -o ./bin/x86/druid'

install: build ## Build and install Druid binaries
	install -m 0755 ./bin/druid /usr/local/bin/druid
	install -m 0755 ./bin/druid-coldstarter /usr/local/bin/druid-coldstarter

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


test-integration: test-integration-docker test-integration-kubernetes

test-integration-docker:
	go test -count=1 -timeout $(INTEGRATION_TIMEOUT) -tags='integration docker' -v ./test/integration/docker

test-integration-kubernetes: kind-integration-up
	go test -count=1 -timeout $(INTEGRATION_TIMEOUT) -tags='integration kubernetes' -v ./test/integration/kubernetes

kind-integration-up:
	@command -v kind >/dev/null 2>&1 || (echo "Installing kind $(KIND_VERSION)..." && go install sigs.k8s.io/kind@$(KIND_VERSION))
	@PATH="$(GO_BIN):$$PATH"; if ! kind get clusters | grep -qx "$(KIND_CLUSTER)"; then kind create cluster --name "$(KIND_CLUSTER)" --wait 120s; fi
	@PATH="$(GO_BIN):$$PATH"; kind export kubeconfig --name "$(KIND_CLUSTER)" >/dev/null
	@kubectl config use-context "kind-$(KIND_CLUSTER)" >/dev/null

kind-integration-down:
	@PATH="$(GO_BIN):$$PATH"; kind delete cluster --name "$(KIND_CLUSTER)"

test-integration-docker-debug:
	docker build . -f Dockerfile.testing -t druid-cli-test
	docker run -v ./:/app -v /var/run/docker.sock:/var/run/docker.sock --entrypoint=/bin/bash --rm -p 2345:2345 -it druid-cli-test -c "dlv --listen=:2345 --headless=true --log=true --log-output=debugger,debuglineerr,gdbwire,lldbout,rpc --accept-multiclient --api-version=2 test --build-flags='-tags=integration docker' ./test/integration/docker"
