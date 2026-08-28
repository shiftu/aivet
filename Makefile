VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
TARGETS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build test release clean install

build:
	@mkdir -p dist
	go build -trimpath -ldflags '$(LDFLAGS)' -o dist/aivet ./cmd/aivet
	@echo "dist/aivet ($(VERSION))"

test:
	go vet ./... && go test ./...

release: test
	@mkdir -p dist
	@for t in $(TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=""; [ $$os = windows ] && ext=.exe; \
	  echo "  $$os/$$arch"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/aivet_$${os}_$${arch}$$ext ./cmd/aivet || exit 1; \
	done
	@cd dist && shasum -a 256 aivet_* > SHA256SUMS && cat SHA256SUMS

install: build
	install -m 0755 dist/aivet $${AIVET_INSTALL_DIR:-$$HOME/.local/bin}/aivet

clean:
	rm -rf dist
