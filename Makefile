BINARY := gh-secretz
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test lint clean install

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

test:
	go test ./... -race

lint:
	go vet ./...
	gofmt -l .

clean:
	rm -f $(BINARY)

install: build
	gh extension install .
