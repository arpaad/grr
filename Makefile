.PHONY: build test test-race cover vet lint tidy fmt ci clean

build:
	go build ./...

test:
	go test ./...

test-race:
	go test ./... -race

cover:
	go test ./... -race -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

vet:
	go vet ./...

lint:
	golangci-lint run

tidy:
	go mod tidy
	git diff --exit-code go.mod go.sum

fmt:
	gofmt -l -w .

ci: tidy vet lint test-race

clean:
	rm -f coverage.out
