BIN_DIR := bin
CMDS := mdtrace
VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test cover lint fmt vet clean install

all: build

build:
	@mkdir -p $(BIN_DIR)
	@for cmd in $(CMDS); do \
		echo "building $$cmd"; \
		go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$$cmd ./cmd/$$cmd || exit 1; \
	done

install:
	@for cmd in $(CMDS); do \
		go install -ldflags "$(LDFLAGS)" ./cmd/$$cmd || exit 1; \
	done

test:
	go test ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

fmt:
	gofmt -l -w .

vet:
	go vet ./...

lint: fmt vet

clean:
	rm -rf $(BIN_DIR) coverage.out
