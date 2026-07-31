.PHONY: build clean run test

# 编译参数
BINARY_COLLECTOR=arp_collector
BINARY_SERVER=arp_server
BUILD_DIR=./bin
GO=go

# 版本信息
VERSION?=0.0.4
BUILD_TIME=$(shell date +%FT%T%z)
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)"

# 默认目标
all: build

# 编译全部
build: build-collector build-server

# 编译 Collector
build-collector:
	@echo ">>> 编译 $(BINARY_COLLECTOR)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_COLLECTOR) ./cmd/collector

# 编译 Server
build-server:
	@echo ">>> 编译 $(BINARY_SERVER)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_SERVER) ./cmd/server

# 静态编译（适用于跨机器部署）
build-static:
	@echo ">>> 静态编译..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_COLLECTOR) ./cmd/collector
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_SERVER) ./cmd/server

# 运行 Server
run-server: build-server
	$(BUILD_DIR)/$(BINARY_SERVER) -config ./config/config.yaml

# 运行 Collector（单机模式）
run-standalone: build-collector
	$(BUILD_DIR)/$(BINARY_COLLECTOR) -config ./config/config.yaml -mode standalone

# 运行 Collector（分布式模式）
run-collector: build-collector
	@if [ -z "$(PROXY)" ]; then \
		echo "Usage: make run-collector PROXY=<proxy_name>"; \
		exit 1; \
	fi
	$(BUILD_DIR)/$(BINARY_COLLECTOR) -config ./config/config.yaml -mode collector -proxy $(PROXY)

# 单次运行
run-once: build-collector
	$(BUILD_DIR)/$(BINARY_COLLECTOR) -config ./config/config.yaml -once

# 单设备测试
test-device: build-collector
	@if [ -z "$(DEVICE)" ]; then \
		echo "Usage: make test-device DEVICE=<ip_or_name>"; \
		exit 1; \
	fi
	$(BUILD_DIR)/$(BINARY_COLLECTOR) -config ./config/config.yaml -device $(DEVICE)

# 下载依赖
deps:
	$(GO) mod download
	$(GO) mod tidy

# 测试
test:
	$(GO) test -v ./...

# 清理
clean:
	@echo ">>> 清理..."
	rm -rf $(BUILD_DIR)

# 格式化
fmt:
	$(GO) fmt ./...

# 检查
vet:
	$(GO) vet ./...

# 帮助
help:
	@echo "可用命令:"
	@echo "  make build           - 编译全部（Collector + Server）"
	@echo "  make build-collector - 编译 Collector"
	@echo "  make build-server    - 编译 Server"
	@echo "  make build-static    - 静态编译（无依赖）"
	@echo ""
	@echo "  make run-server      - 运行 Server（提供 API）"
	@echo "  make run-standalone  - 运行 Collector（单机模式）"
	@echo "  make run-collector PROXY=<name> - 运行分布式 Collector"
	@echo "  make run-once        - 单次运行后退出"
	@echo "  make test-device DEVICE=<ip> - 单设备测试"
	@echo ""
	@echo "  make deps            - 下载依赖"
	@echo "  make test            - 运行测试"
	@echo "  make clean           - 清理"
	@echo ""
	@echo "分布式部署示例:"
	@echo "  1. 在 Server 节点运行: make run-server"
	@echo "  2. 在各 Proxy 节点运行: make run-collector PROXY=proxy1"
