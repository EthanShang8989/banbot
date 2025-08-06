#!/bin/bash

echo "构建测试程序..."
cd /home/ethan/Dev/banbot-project/banbot
go build -o test_download cmd/test_download/main.go

if [ $? -ne 0 ]; then
    echo "构建失败!"
    exit 1
fi

echo ""
echo "运行下载测试..."
echo ""

# 测试1：下载现货单天数据
echo "========== 测试1：下载现货单天数据 =========="
./test_download -symbol BTCUSDT -date 2024-01-01 -days 1 -market spot

echo ""
echo ""

# 测试2：下载合约数据
echo "========== 测试2：下载合约3天数据 =========="
./test_download -symbol BTCUSDT -date 2024-01-01 -days 3 -market futures

echo ""
echo ""

# 测试3：下载多个现货交易对
echo "========== 测试3：下载ETHUSDT现货数据 =========="
./test_download -symbol ETHUSDT -date 2024-01-01 -days 1 -market spot

echo ""
echo ""

# 测试4：使用默认参数（昨天的现货数据）
echo "========== 测试4：下载昨天的现货数据 =========="
./test_download -symbol BTCUSDT -market spot

echo ""
echo "所有测试完成!"