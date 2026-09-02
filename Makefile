.PHONY: build test web format dev
build: web
	mkdir -p bin
	go build -trimpath -o bin/relay ./cmd/relay
web:
	npm ci --prefix web
	npm run build --prefix web
test:
	go test -race ./...
	npm test --prefix web
format:
	gofmt -w cmd internal migrations
	npm run format --prefix web
dev:
	go run ./cmd/relay serve
