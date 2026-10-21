.PHONY: init dev build test check docker
init:
	go run ./cmd/drive init
dev:
	go run ./cmd/drive
build:
	cd web && npm ci && npm run build
	go build -trimpath -o bin/drive ./cmd/drive
test:
	go test ./...
	go test -race ./...
	cd web && npm test && npm run typecheck && npm run build
check:
	go vet ./...
	python3 -m unittest discover -s scripts -p '*_test.py'
docker:
	docker compose build
