.PHONY: hb test
hb:
	go build -mod=mod -o bin/hb ./cmd/hb

test:
	go test -mod=mod ./cmd/... ./internal/...
