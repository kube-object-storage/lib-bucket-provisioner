all: build fmt imports vet lint test
	@echo "all - done."
.PHONY: all

vendor:
	dep ensure
	@echo "vendor - done."
.PHONY: vendor

build: vendor
	go build ./pkg/...
	@echo "build - done"
.PHONY: build

fmt:
	go fmt ./pkg/...
	@echo "fmt - done."
.PHONY: fmt

imports:
	go get -u golang.org/x/tools/cmd/goimports
	goimports -w ./pkg/
	@echo "imports - done."
.PHONY: imports

vet:
	go vet ./pkg/...
	@echo "vet - done."
.PHONY: vet

lint: vendor
	go get -u golang.org/x/lint/golint
	golint -set_exit_status=1 ./pkg/...
	@echo "lint - done."
.PHONY: lint

test: vendor
	go test ./pkg/...
	@echo "test - done."
.PHONY: test

clean:
	rm -rf ./vendor/
	@echo "clean - done."
.PHONY: clean

