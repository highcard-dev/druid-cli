module github.com/highcard-dev/plugin

go 1.19

require (
	github.com/gorilla/websocket v1.5.1
	github.com/hashicorp/go-plugin v1.6.0
	github.com/highcard-dev/gorcon v1.3.10
	golang.org/x/net v0.20.0
	google.golang.org/grpc v1.60.1
)

require (
	github.com/fatih/color v1.15.0 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/hashicorp/go-hclog v1.6.2 // indirect
	github.com/hashicorp/yamux v0.1.1 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/go-testing-interface v1.14.1 // indirect
	github.com/oklog/run v1.1.0 // indirect
	github.com/rogpeppe/go-internal v1.10.0 // indirect
	golang.org/x/sys v0.16.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20231120223509-83a465c0220f // indirect
	google.golang.org/protobuf v1.32.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

require (
	github.com/highcard-dev/proto v0.0.0
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.26.0 // indirect
)

require github.com/highcard-dev/logger v0.0.0

require (
	github.com/highcard-dev/env v0.0.0 // indirect
	gopkg.in/yaml.v2 v2.4.0
)

replace github.com/highcard-dev/logger => ../../../../shared/golang/logger

replace github.com/highcard-dev/env => ../../../../shared/golang/env

replace github.com/highcard-dev/proto => ../../../../shared/proto
