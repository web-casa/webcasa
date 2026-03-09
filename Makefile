.PHONY: all build dev clean frontend backend run seed-data

# Default target
all: build

# Build everything (frontend + backend)
build: frontend backend
	@echo "✅ Build complete: ./webcasa"

# Build backend only
backend:
	@echo "🔨 Building Go backend..."
	CGO_ENABLED=1 go build -o webcasa .

# Build frontend
frontend:
	@echo "🔨 Building Vue frontend..."
	cd web && npm install && npm run build

# Run in development mode (backend only, frontend served by Vite)
dev:
	@echo "🚀 Starting backend in dev mode..."
	@echo "   Run 'cd web && npm run dev' in another terminal for the frontend"
	WEBCASA_PORT=8080 go run .

# Run the built binary
run: build
	./webcasa

# Clean build artifacts
clean:
	rm -f webcasa
	rm -rf web/dist
	rm -rf web/node_modules

# Install frontend dependencies
install-frontend:
	cd web && npm install

# Docker build
docker-build:
	docker build -t webcasa .

# Docker run
docker-run:
	docker run -d \
		--name webcasa \
		-p 8080:8080 \
		-p 80:80 \
		-p 443:443 \
		-v webcasa-data:/app/data \
		webcasa

# Run tests
test:
	go test ./... -v

# Generate app store seed data from upstream repo
APPSTORE_REPO ?= https://github.com/web-casa/appstore
seed-data:
	@echo "📦 Generating app store seed data..."
	@rm -rf /tmp/webcasa-appstore-seed
	@git clone --depth 1 --branch master $(APPSTORE_REPO) /tmp/webcasa-appstore-seed
	@go run ./plugins/appstore/cmd/seedgen /tmp/webcasa-appstore-seed plugins/appstore/seed_apps.json.gz
	@rm -rf /tmp/webcasa-appstore-seed
	@echo "✅ Seed data generated: $$(ls -lh plugins/appstore/seed_apps.json.gz | awk '{print $$5}')"

# Format code
fmt:
	go fmt ./...
	cd web && npx prettier --write src/
