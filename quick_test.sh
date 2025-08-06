#!/bin/bash

echo "快速测试逐笔交易数据下载功能"
echo "=============================="
echo ""

cd /home/ethan/Dev/banbot-project/banbot

# 编译测试程序
echo "编译测试程序..."
go build -o test_dl cmd/test_download/main.go

if [ $? -ne 0 ]; then
    echo "❌ 编译失败!"
    exit 1
fi

echo "✅ 编译成功"
echo ""

# 测试下载一天的数据
echo "测试下载 BTCUSDT 2024-01-01 的数据..."
echo "----------------------------------------"
./test_dl -symbol BTCUSDT -date 2024-01-01 -days 1 -dir /tmp/banbot_quick_test

echo ""
echo "=============================="
echo "测试完成!"
echo ""
echo "如果下载成功，您可以："
echo "1. 查看下载的文件: ls -la /tmp/banbot_quick_test/trades/BTCUSDT/"
echo "2. 运行更多测试: ./test_download.sh"
echo "3. 运行集成测试: go test -v ./data -run TestTradeDataIntegration"