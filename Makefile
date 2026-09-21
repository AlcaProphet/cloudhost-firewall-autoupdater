VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/alcaprophet/cloudhost-firewall-autoupdater/version.Version=$(VERSION)

.PHONY: build test vet clean docker-build all frontend

frontend:
	cd webui/frontend && npm ci && npm run build

build: frontend
	go build -ldflags="$(LDFLAGS)" -o fwalizer .

test:
	go test ./... -v

vet:
	go vet ./...

clean:
	rm -f fwalizer

docker-build:
	docker build -f build/Dockerfile --build-arg VERSION=$(VERSION) -t fwalizer .

all: vet test build
