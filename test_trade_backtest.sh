#!/bin/bash

# Test script for trade data backtesting
# 测试逐笔交易数据回测的脚本

echo "Starting trade data backtest test..."
echo "开始测试逐笔交易数据回测..."

# Build the project
echo "Building banbot..."
go build -o banbot main.go
if [ $? -ne 0 ]; then
    echo "Build failed! 构建失败！"
    exit 1
fi

# Run backtest with trade data
echo "Running backtest with trade data..."
echo "运行带逐笔交易数据的回测..."

./banbot backtest -config test_trade_config.yml -v

echo "Test completed! 测试完成！"
echo "Check the output for trade data processing logs."
echo "检查输出日志中的逐笔交易数据处理记录。"