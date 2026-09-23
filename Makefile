.PHONY: build test vet clean docker-build all frontend

frontend:
	cd webui/frontend && npm ci && npm run build

build: frontend
	go build -o fwalizer .

test:
	go test ./... -v

vet:
	go vet ./...

clean:
	rm -f fwalizer

docker-build:
	docker build -f build/Dockerfile -t fwalizer .

all: vet test build
