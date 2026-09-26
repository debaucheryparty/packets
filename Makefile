.PHONY: build release test e2e lint fmt dev-up dev-down

build:
	go build -ldflags "-s -w -X main.version=$$(git describe --tags --always --dirty)" -o bin/packets ./cmd/packets
	go build -ldflags "-s -w -X main.version=$$(git describe --tags --always --dirty)" -o bin/packetsd ./cmd/packetsd

release:
	bash scripts/build_release.sh

test:
	go test ./... -race

e2e:
	go test ./scripts/e2e/... -v

lint:
	golangci-lint run
	gofumpt -l .

fmt:
	gofumpt -w .

dev-up:
	docker-compose -f deploy/docker-compose.yml up -d

dev-down:
	docker-compose -f deploy/docker-compose.yml down
