# Ironsight

.PHONY: build run dev clean docker-db docker-down

# Build the server binary
build:
	go build -o bin/ironsight.exe ./cmd/server

# Run the server
run: build
	./bin/ironsight.exe

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

# Regenerate generated docs (route/API coverage matrix + registry rollup)
docs-gen:
	go run ./cmd/docgen -write

# Verify generated docs are current + lint the feature registry
docs-check:
	go run ./cmd/docgen -check
