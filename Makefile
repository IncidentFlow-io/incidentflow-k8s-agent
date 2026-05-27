BINARY ?= incidentflow-k8s-agent
IMAGE ?= incidentflow/incidentflow-k8s-agent
TAG ?= dev

.PHONY: build test docker-build run lint kind-smoke-test kind-delete

build:
	go build -o bin/$(BINARY) ./cmd/agent

test:
	go test ./...

docker-build:
	docker build -t $(IMAGE):$(TAG) .

run:
	go run ./cmd/agent

lint:
	find . -path './.cache' -prune -o -name '*.go' -print0 | xargs -0 gofmt -w
	go vet ./...

kind-smoke-test:
	./scripts/kind-smoke-test.sh

kind-delete:
	kind delete cluster --name $${KIND_CLUSTER_NAME:-incidentflow-agent}
