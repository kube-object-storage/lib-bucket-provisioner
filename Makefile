all: fmt lint build test
	@echo "✅ all - done"
.PHONY: all

fmt:
	go fmt ./pkg/...
	go get -u golang.org/x/tools/cmd/goimports
	goimports -w ./pkg/
	@echo "✅ fmt - done"
.PHONY: fmt

lint:
	go vet ./pkg/...
	go get -u golang.org/x/lint/golint
	golint -set_exit_status=1 ./pkg/...
	@echo "✅ lint - done"
.PHONY: lint

build: vendor
	go build ./pkg/...
	@echo "✅ build - done"
.PHONY: build

test: vendor
	go test ./pkg/...
	@echo "✅ test - done"
.PHONY: test

vendor:
	dep ensure
	@echo "✅ vendor - done"
.PHONY: vendor

gen-api: vendor
	go get -u k8s.io/code-generator
	./vendor/k8s.io/code-generator/generate-groups.sh all \
		github.com/kube-object-storage/lib-bucket-provisioner/pkg/client \
		github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis \
		objectbucket.io:v1alpha1
.PHONY: gen-api

# fail-if-diff is a CI task to verify we committed everything
fail-if-diff:
	git diff --exit-code || ( \
		echo ""; \
		echo "❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌"; \
		echo ""; \
		echo "❌ ERROR: Sources changed on build"; \
		echo ""; \
		echo "You should consider:";  \
		echo "  🚩 make ";  \
		echo "  🚩 git commit -a [--amend] ";  \
		echo "  🚩 git push [-f] ";  \
		echo ""; \
		echo "❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌❌"; \
		echo ""; \
		exit 1; \
	)
.PHONY: fail-if-diff

clean:
	rm -rf ./vendor/
	@echo "✅ clean - done"
.PHONY: clean
