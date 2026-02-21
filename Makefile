.PHONY: all build dev clean frontend backend run

# Default target
all: build

# Build everything (frontend + backend)
build: frontend backend
	@echo "✅ Build complete: ./caddypanel"

# Build backend only
backend:
	@echo "🔨 Building Go backend..."
	CGO_ENABLED=1 go build -o caddypanel .

# Build frontend
frontend:
	@echo "🔨 Building Vue frontend..."
	cd web && npm install && npm run build

# Run in development mode (backend only, frontend served by Vite)
dev:
	@echo "🚀 Starting backend in dev mode..."
	@echo "   Run 'cd web && npm run dev' in another terminal for the frontend"
	CADDYPANEL_PORT=8080 go run .

# Run the built binary
run: build
	./caddypanel

# Clean build artifacts
clean:
	rm -f caddypanel
	rm -rf web/dist
	rm -rf web/node_modules

# Install frontend dependencies
install-frontend:
	cd web && npm install

# Docker build
docker-build:
	docker build -t caddypanel .

# Docker run
docker-run:
	docker run -d \
		--name caddypanel \
		-p 8080:8080 \
		-p 80:80 \
		-p 443:443 \
		-v caddypanel-data:/app/data \
		caddypanel

# Run tests
test:
	go test ./... -v

# Format code
fmt:
	go fmt ./...
	cd web && npx prettier --write src/
