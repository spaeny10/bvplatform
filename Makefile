# ONVIF Tool

.PHONY: build run dev clean docker-db docker-down

# Build the server binary
build:
	go build -o bin/onvif-tool.exe ./cmd/server

# Run the server
run: build
	./bin/onvif-tool.exe

# Run with live reload (requires air: go install github.com/cosmtrek/air@latest)
dev:
	air

# Start the database
docker-db:
	docker-compose up -d

# Stop the database
docker-down:
	docker-compose down

# Clean build artifacts
clean:
	rm -rf bin/
	rm -rf storage/

# Run tests
test:
	go test ./...

# Download dependencies
deps:
	go mod tidy
	go mod download
