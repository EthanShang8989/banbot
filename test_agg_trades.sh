#!/bin/bash

echo "========================================"
echo "测试 Binance 聚合逐笔交易数据下载"
echo "========================================"
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

# 测试现货聚合交易数据
echo "1. 测试现货聚合交易数据下载"
echo "----------------------------------------"
./test_dl -symbol BTCUSDT -date 2025-08-03 -days 1 -market spot -dir /tmp/banbot_agg_test
echo ""

# 测试合约聚合交易数据
echo "2. 测试合约聚合交易数据下载"
echo "----------------------------------------"
./test_dl -symbol BTCUSDT -date 2025-08-04 -days 1 -market futures -dir /tmp/banbot_agg_test
echo ""

echo "========================================"
echo "查看下载的文件:"
echo "----------------------------------------"
echo "现货文件:"
ls -la /tmp/banbot_agg_test/trades/spot/BTCUSDT/ 2>/dev/null || echo "  (无文件)"
echo ""
echo "合约文件:"
ls -la /tmp/banbot_agg_test/trades/futures/BTCUSDT/ 2>/dev/null || echo "  (无文件)"
echo ""

echo "========================================"
echo "测试完成!"
echo ""
echo "提示："
echo "- 如果下载失败，请检查日期是否有效（不能是未来日期）"
echo "- 现货路径: /tmp/banbot_agg_test/trades/spot/"
echo "- 合约路径: /tmp/banbot_agg_test/trades/futures/"
echo "========================================"