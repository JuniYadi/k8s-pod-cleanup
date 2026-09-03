.PHONY: all test build docker-build run

all: test build

test:
	cd src && go test -v -race ./...

build:
	cd src && CGO_ENABLED=0 go build -o ../bin/k8s-pod-cleanup ./cmd/cleaner

docker-build:
	docker build -t k8s-pod-cleanup:latest .

run:
	cd src && go run ./cmd/cleaner --dry-run=true
