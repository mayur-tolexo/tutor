.PHONY: build web test validate run lint docker

## build: build the web app and the Go binary that embeds it
build: web
	go build -o bin/tutor ./cmd/tutor

## web: build the PWA into web/dist
web:
	cd web && npm ci && npm run build

## test: Go unit tests (Postgres conformance runs when TUTOR_TEST_DATABASE_URL is set) and web tests
test:
	go test ./...
	cd web && npm test -- --run

## validate: check every exercise's solution, tests, and hints
validate:
	go run ./cmd/tutor content validate

## run: start the server against local python3 and an in-memory store
run:
	INSECURE_COOKIES=1 go run ./cmd/tutor serve

## lint: gofmt and vet
lint:
	test -z "$$(gofmt -l cmd internal content web)"
	go vet ./...

## docker: build the production image
docker:
	docker build -f deploy/Dockerfile -t tutor:local .
