BIN := swarm-up

.PHONY: build test fmt vet install clean

build:
	go build -o $(BIN) .

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

install:
	go install .

clean:
	rm -f $(BIN)
