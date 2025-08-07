#!/bin/bash

# BanBot Test Script
# Unified test script for all test scenarios

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test modes
MODE=${1:-"all"}

function print_header() {
    echo -e "${GREEN}=========================================${NC}"
    echo -e "${GREEN}$1${NC}"
    echo -e "${GREEN}=========================================${NC}"
}

function print_error() {
    echo -e "${RED}[ERROR] $1${NC}"
}

function print_info() {
    echo -e "${YELLOW}[INFO] $1${NC}"
}

# Quick test - run unit tests
function run_quick_test() {
    print_header "Running Quick Tests"
    go test -short ./...
}

# Download test - test trade data download
function run_download_test() {
    print_header "Testing Trade Data Download"
    
    # Test download for a specific date
    go test -v -run TestBinanceDataDownloader ./data
    
    # Test with real download (optional)
    if [ "$2" == "real" ]; then
        print_info "Testing real download from Binance..."
        go run cmd/test_download/main.go
    fi
}

# Cache test - test cache functionality
function run_cache_test() {
    print_header "Testing Cache System"
    go test -v -run "TestCache|TestBinary|TestOptimized" ./data
}

# Integration test - full integration test
function run_integration_test() {
    print_header "Running Integration Tests"
    go test -v -run TestTradeDataIntegration ./data
}

# Backtest - run backtest with trade data
function run_backtest() {
    print_header "Running Backtest with Trade Data"
    
    CONFIG_FILE=${2:-"test_trade_config.yml"}
    
    if [ ! -f "$CONFIG_FILE" ]; then
        print_error "Config file not found: $CONFIG_FILE"
        exit 1
    fi
    
    print_info "Using config: $CONFIG_FILE"
    go run main.go backtest -config "$CONFIG_FILE"
}

# Performance test - run benchmarks
function run_performance_test() {
    print_header "Running Performance Tests"
    go test -bench=. -benchtime=10s ./data
}

# Full test - run all tests
function run_all_tests() {
    print_header "Running All Tests"
    
    run_quick_test
    echo ""
    
    run_cache_test
    echo ""
    
    run_integration_test
    echo ""
    
    run_performance_test
}

# Clean - clean test data and cache
function clean_test_data() {
    print_header "Cleaning Test Data"
    
    print_info "Removing cache directories..."
    rm -rf /tmp/banbot_*
    rm -rf /tmp/bench_*
    rm -rf /tmp/test_*
    
    print_info "Cleaning local test files..."
    find . -name "*.test" -delete
    find . -name "*.out" -delete
    
    print_info "Clean complete!"
}

# Show help
function show_help() {
    echo "BanBot Test Script"
    echo ""
    echo "Usage: ./test.sh [mode] [options]"
    echo ""
    echo "Modes:"
    echo "  quick       - Run quick unit tests"
    echo "  download    - Test trade data download"
    echo "  cache       - Test cache system"
    echo "  integration - Run integration tests"
    echo "  backtest    - Run backtest with trade data"
    echo "  performance - Run performance benchmarks"
    echo "  all         - Run all tests (default)"
    echo "  clean       - Clean test data and cache"
    echo "  help        - Show this help message"
    echo ""
    echo "Examples:"
    echo "  ./test.sh                    # Run all tests"
    echo "  ./test.sh quick              # Run quick tests only"
    echo "  ./test.sh download real      # Test download with real data"
    echo "  ./test.sh backtest config.yml # Run backtest with specific config"
    echo "  ./test.sh clean              # Clean all test data"
}

# Main execution
case $MODE in
    quick)
        run_quick_test
        ;;
    download)
        run_download_test "$@"
        ;;
    cache)
        run_cache_test
        ;;
    integration)
        run_integration_test
        ;;
    backtest)
        run_backtest "$@"
        ;;
    performance)
        run_performance_test
        ;;
    all)
        run_all_tests
        ;;
    clean)
        clean_test_data
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        print_error "Unknown mode: $MODE"
        echo ""
        show_help
        exit 1
        ;;
esac

echo ""
print_info "Test completed!"