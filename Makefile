.PHONY: build test vet clean docker-build all frontend frontend-test

frontend:
	cd webui/frontend && npm ci && npm run build

build: frontend
	go build -o fwalizer .

frontend-test: frontend
	cd webui/frontend && npm test

test: frontend-test
	go test ./... -race -v

vet: frontend
	go vet ./...

clean:
	rm -f fwalizer

docker-build:
	docker build -f build/Dockerfile -t fwalizer .

all: vet test build
