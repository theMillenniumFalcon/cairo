BINARY  := cairo
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build install clean test lint run linux darwin windows all gitignore

## build: compile for current platform
build:
	go build $(LDFLAGS) -o $(BINARY) .

## run: build and run
run: build
	./$(BINARY)

## install: install to GOPATH/bin
install:
	go install $(LDFLAGS) .

## test: run all tests
test:
	go test ./...

## lint: vet + check for issues
lint:
	go vet ./...

## linux: cross-compile for Linux amd64
linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY)-linux-amd64 .

## darwin-arm: cross-compile for macOS Apple Silicon
darwin:
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY)-darwin-arm64 .

## darwin-amd: cross-compile for macOS Intel
darwin-amd:
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY)-darwin-amd64 .

## windows: cross-compile for Windows amd64
windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY)-windows-amd64.exe .

## all: build for all platforms
all: linux darwin darwin-amd windows

## gitignore: create a .gitignore if one doesn't exist
gitignore:
	@if [ ! -f .gitignore ]; then \
		printf '.env\n.cairo/\n$(BINARY)\n$(BINARY)-*\n*.exe\n' > .gitignore; \
		echo "Created .gitignore"; \
	else \
		echo ".gitignore already exists"; \
	fi

## clean: remove build artifacts
clean:
	rm -f $(BINARY) $(BINARY)-* *.exe