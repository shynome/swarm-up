BIN := swarm-up
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test fmt vet install version clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) .

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

install:
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" .

version:
	@echo $(VERSION)

clean:
	rm -f $(BIN)
