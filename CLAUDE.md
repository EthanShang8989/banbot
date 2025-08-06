# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Language Preference

**重要**: 在与用户交互时，请使用中文回答。代码注释和文档可以保持英文。

## Project Overview

BanBot is a high-performance event-driven cryptocurrency trading bot written in Go. It supports multiple exchanges, strategies, timeframes, and accounts with features for backtesting, live trading, and hyperparameter optimization.

## Build and Development Commands

### Go Backend
```bash
# Build the main binary
go build -o banbot main.go

# Run tests
go test ./...

# Run specific package tests
go test ./biz/...
go test ./orm/...
go test ./utils/...

# Run with race detection
go test -race ./...

# Format code
go fmt ./...

# Vet code
go vet ./...
```

### Frontend (SvelteKit UI)
```bash
# Navigate to UI directory
cd web/ui

# Install dependencies
npm install

# Development server
npm run dev

# Build production
npm run build

# Preview production build
npm run preview

# Type checking
npm run check

# Linting
npm run lint

# Format code
npm run format
```

### Docker
```bash
# Build Docker image
docker build -f docker/Dockerfile -t banbot .

# Run with TimescaleDB
docker network create mynet
docker run -d --name timescaledb --network mynet -p 127.0.0.1:5432:5432 \
  -v /opt/pgdata:/var/lib/postgresql/data \
  -e POSTGRES_PASSWORD=123 timescale/timescaledb:latest-pg17

# Run BanBot container
docker run -d --name banbot -p 8000:8000 --network mynet -v /root:/root banbot/banbot:latest -config /root/config.yml
```

## Core Architecture

### Package Structure

- **`entry/`**: Application entry point and command-line interface
  - Main command routing in `entry.go`
  - Handles backtest, live trading, data download modes

- **`biz/`**: Business logic layer
  - `trader.go`: Core trading logic
  - `odmgr*.go`: Order management (live/local variants)
  - `wallet.go`: Wallet and balance management
  - `data_server.go`: Data serving functionality

- **`core/`**: Core types and utilities
  - Defines fundamental types like `Order`, `Bar`, `TimeFrame`
  - Common calculations and utilities

- **`orm/`**: Database layer (PostgreSQL/TimescaleDB)
  - `kline.go`: K-line (candlestick) data management
  - `models.go`: Database models
  - SQL queries in `sql/` directory
  - SQLC generated code for type-safe queries

- **`strat/`**: Strategy framework
  - Base strategy interfaces and implementations
  - Strategy lifecycle management

- **`live/`**: Live trading components
  - `crypto_trader.go`: Main live trading coordinator

- **`opt/`**: Optimization and backtesting
  - `backtest.go`: Backtesting engine
  - `hyper_opt.go`: Hyperparameter optimization
  - Supports multiple samplers: bayes, tpe, cmaes variants

- **`data/`**: Data fetching and management
  - `spider.go`: Historical data collection
  - `feeder.go`: Real-time data feeding
  - `watcher.go`: Market monitoring

- **`exg/`**: Exchange integrations
  - Wrapper around banexg library
  - Unified exchange interface

- **`web/`**: Web interfaces
  - `ui/`: SvelteKit frontend application
  - `live/`: Live trading API endpoints
  - `dev/`: Development/debugging endpoints

- **`rpc/`**: Remote procedure calls and notifications
  - Email, webhook, WeChat Work integrations

## Key Concepts

### Event-Driven Architecture
- No lookahead in backtesting to ensure realistic simulations
- Events processed in chronological order
- Same code paths for backtesting and live trading

### Multi-Level Configuration
- YAML configuration with merge support
- Environment-specific configs (config.yml, config.local.yml)
- Per-strategy parameter definitions

### Order Management
- Unified order interface for backtesting and live trading
- Support for different order types and directions
- Position tracking and management

### Data Management
- TimescaleDB for time-series data storage
- Efficient K-line data compression with Protocol Buffers
- Support for multiple timeframes (1m, 5m, 15m, 1h, 4h, 1d, etc.)

## Database Schema

The project uses PostgreSQL/TimescaleDB with migrations in `orm/sql/`:
- `pg_schema.sql`: Main schema definitions
- `pg_migrations.sql`: Schema migrations
- `trade_schema.sql`: Trading-specific tables
- `ui_schema.sql`: UI-related tables

## Testing Approach

Tests are located alongside their respective packages:
- Unit tests: `*_test.go` files
- Run all tests: `go test ./...`
- Run with coverage: `go test -cover ./...`

## Dependencies

Key dependencies (from go.mod):
- `github.com/banbox/banexg`: Exchange connectivity
- `github.com/banbox/banta`: Technical analysis library
- `github.com/jackc/pgx/v5`: PostgreSQL driver
- `github.com/gofiber/fiber/v2`: Web framework
- `github.com/c-bata/goptuna`: Hyperparameter optimization
- `google.golang.org/grpc`: gRPC for inter-service communication

## Configuration Files

- `/root/config.yml`: Main configuration (API keys, database URL, accounts)
- `biz/config.yml`: Default business logic configuration
- `biz/config.local.yml`: Local overrides (gitignored)

## Common Development Tasks

### Running Backtests
```bash
banbot backtest -config config.yml
```

### Starting Live Trading
```bash
banbot trade -config config.yml
```

### Downloading Historical Data
```bash
banbot down -pairs BTC/USDT,ETH/USDT -start 2024-01-01
```

### Hyperparameter Optimization
```bash
banbot optimize -opt-rounds 40 -concur 3 -sampler bayes
```

### Collecting Optimization Results
```bash
banbot collect_opt -in [optimization_output_dir]
```

## Network Control

Use `-net-off` parameter to disable network requests for offline mode testing.