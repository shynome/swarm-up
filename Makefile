BIN := swarm-up
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test fmt vet install version dist clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) .

# 本地打 linux/amd64 + linux/arm64 发布包（与 GitHub Actions 一致）
dist:
	rm -rf dist && mkdir -p dist
	@for arch in amd64 arm64; do \
		echo "building linux/$$arch"; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/swarm-up . || exit 1; \
		tar -C dist -czf "dist/swarm-up_$(VERSION)_linux_$$arch.tar.gz" swarm-up; \
	done
	cd dist && sha256sum *.tar.gz > checksums.txt
	@ls -l dist

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
