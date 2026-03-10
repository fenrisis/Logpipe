.PHONY: build install install-service uninstall clean test run

BINARY_NAME = logpipe
BUILD_DIR = bin
PREFIX = /usr/local

# Detect OS
UNAME_S := $(shell uname -s)

# CGO flags for SQLite FTS5 support
export CGO_CFLAGS = -DSQLITE_ENABLE_FTS5

# Build
build:
	@echo "Building $(BINARY_NAME)..."
	CGO_ENABLED=1 go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/logpipe
	@echo "Done: $(BUILD_DIR)/$(BINARY_NAME)"

# Install binary
install: build
	@echo "Installing $(BINARY_NAME) to $(PREFIX)/bin..."
	@mkdir -p $(PREFIX)/bin
	cp $(BUILD_DIR)/$(BINARY_NAME) $(PREFIX)/bin/
	@echo "Done"

# Install as service (macOS or Linux)
install-service: install
ifeq ($(UNAME_S),Darwin)
	@echo "Installing launchd service..."
	@mkdir -p /usr/local/var/log
	@mkdir -p /usr/local/var/logpipe
	cp init/com.logpipe.server.plist ~/Library/LaunchAgents/
	launchctl load ~/Library/LaunchAgents/com.logpipe.server.plist
	@echo "Service installed. Check status with: launchctl list | grep logpipe"
else
	@echo "Installing systemd user service..."
	@mkdir -p ~/.config/systemd/user
	cp init/logpipe.service ~/.config/systemd/user/
	systemctl --user daemon-reload
	systemctl --user enable logpipe
	systemctl --user start logpipe
	@echo "Service installed. Check status with: systemctl --user status logpipe"
endif

# Uninstall service
uninstall-service:
ifeq ($(UNAME_S),Darwin)
	@echo "Uninstalling launchd service..."
	-launchctl unload ~/Library/LaunchAgents/com.logpipe.server.plist 2>/dev/null
	-rm -f ~/Library/LaunchAgents/com.logpipe.server.plist
	@echo "Service uninstalled"
else
	@echo "Uninstalling systemd service..."
	-systemctl --user stop logpipe 2>/dev/null
	-systemctl --user disable logpipe 2>/dev/null
	-rm -f ~/.config/systemd/user/logpipe.service
	-systemctl --user daemon-reload
	@echo "Service uninstalled"
endif

# Uninstall everything
uninstall: uninstall-service
	@echo "Removing binary..."
	-rm -f $(PREFIX)/bin/$(BINARY_NAME)
	@echo "Done"

# Install Python SDK
install-python:
	@echo "Installing Python SDK..."
	cd python && pip install -e .
	@echo "Done"

# Build release binaries for all platforms
release:
	@echo "Building release binaries..."
	@mkdir -p $(BUILD_DIR)/release
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o $(BUILD_DIR)/release/$(BINARY_NAME)-darwin-arm64 ./cmd/logpipe
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o $(BUILD_DIR)/release/$(BINARY_NAME)-darwin-amd64 ./cmd/logpipe
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(BUILD_DIR)/release/$(BINARY_NAME)-linux-amd64 ./cmd/logpipe
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o $(BUILD_DIR)/release/$(BINARY_NAME)-linux-arm64 ./cmd/logpipe
	@echo "Done. Binaries in $(BUILD_DIR)/release/"
	@ls -lh $(BUILD_DIR)/release/

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)

# Clean all data
clean-data:
	rm -rf ~/.logpipe

# Run server (foreground)
run: build
	./$(BUILD_DIR)/$(BINARY_NAME) server

# Run TUI
tui: build
	./$(BUILD_DIR)/$(BINARY_NAME)

# Tail logs
logs: build
	./$(BUILD_DIR)/$(BINARY_NAME) logs -f

# Run tests
test:
	go test ./...

# End-to-end test
test-e2e: build
	@echo "Starting server..."
	@./$(BUILD_DIR)/$(BINARY_NAME) server &
	@sleep 2
	@echo "Sending test logs..."
	@echo '{"namespace":"test","service":"demo","level":"INFO","message":"Hello from make test-e2e"}' | nc localhost 5555
	@sleep 1
	@echo "Checking stats..."
	@./$(BUILD_DIR)/$(BINARY_NAME) stats
	@pkill -f "$(BINARY_NAME) server" || true
	@echo "Test complete"

# Show help
help:
	@echo "Logpipe Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build           Build the binary"
	@echo "  make install         Install binary to $(PREFIX)/bin"
	@echo "  make install-service Install as system service (launchd/systemd)"
	@echo "  make uninstall       Uninstall binary and service"
	@echo "  make install-python  Install Python SDK"
	@echo "  make run             Run server in foreground"
	@echo "  make tui             Launch TUI"
	@echo "  make logs            Tail logs"
	@echo "  make clean           Clean build artifacts"
	@echo "  make clean-data      Remove all logpipe data"
	@echo "  make test            Run tests"
	@echo "  make test-e2e        Run end-to-end test"
