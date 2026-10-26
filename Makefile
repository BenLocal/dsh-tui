.PHONY: build test fmt lint check run install clean

BIN := bin/dsh-tui

build:
	go build -o $(BIN) .

test:
	go vet ./...
	go test -race ./...

fmt:
	gofmt -l -w .

lint:
	golangci-lint run

# Everything a commit should pass.
check: fmt lint test

# Pass flags through ARGS, e.g. make run ARGS="-mode read-only".
run:
	go run . $(ARGS)

# Installs to $GOBIN (~/go/bin), which is on PATH.
install:
	go install .

clean:
	rm -rf bin
