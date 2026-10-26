.PHONY: build test install

build:
	go build -o bin/dsh-tui .

test:
	go vet ./...
	go test -race ./...

# Installs to $GOBIN (~/go/bin), which is on PATH.
install:
	go install .
