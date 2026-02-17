generate:
	go generate ./...

build: generate
	go build -o ht ./cmd/ht

test:
	go test ./...

check: check-codegen check-fix check-format check-vet check-staticcheck check-deadcode

check-codegen:
	go generate ./...
	git diff --exit-code

check-fix:
	go fix ./...
	git diff --exit-code

check-format:
	go tool gofumpt -w .
	go tool goimports -w .
	git diff --exit-code

check-vet:
	go vet ./...

check-staticcheck:
	go tool staticcheck ./...

check-deadcode:
	test -z "$(go tool deadcode -test ./...)"
