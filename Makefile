.PHONY: build release test e2e lint fmt dev-up dev-down

build:
	go build -ldflags "-s -w -X main.version=$$(git describe --tags --always --dirty)" -o bin/packets ./cmd/packets
	go build -ldflags "-s -w -X main.version=$$(git describe --tags --always --dirty)" -o bin/packetsd ./cmd/packetsd

release:
	GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o bin/packets-linux-amd64 ./cmd/packets
	GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o bin/packets-linux-arm64 ./cmd/packets
	GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o bin/packets-darwin-amd64 ./cmd/packets
	GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o bin/packets-darwin-arm64 ./cmd/packets
	# same for packetsd
	GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o bin/packetsd-linux-amd64 ./cmd/packetsd
	GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o bin/packetsd-linux-arm64 ./cmd/packetsd
	GOOS=darwin GOARCH=amd64 go build -ldflags "-s -w" -o bin/packetsd-darwin-amd64 ./cmd/packetsd
	GOOS=darwin GOARCH=arm64 go build -ldflags "-s -w" -o bin/packetsd-darwin-arm64 ./cmd/packetsd

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
