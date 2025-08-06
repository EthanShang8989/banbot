#!/bin/bash

# 测试逐笔交易数据下载功能
echo "==================================="
echo "测试 Binance 逐笔交易数据下载功能"
echo "==================================="

cd /home/ethan/Dev/banbot-project/banbot

# 运行所有测试
echo ""
echo "1. 运行完整测试套件..."
echo "--------------------------"
go test -v ./data -run TestBinanceDataDownloader

# 运行单日下载测试
echo ""
echo "2. 测试单日数据下载..."
echo "--------------------------"
go test -v ./data -run TestDownloadSingleDay

# 运行性能测试
echo ""
echo "3. 运行性能基准测试..."
echo "--------------------------"
go test -bench=BenchmarkLoadCompressedCSV ./data -run=^$

echo ""
echo "==================================="
echo "测试完成！"
echo "==================================="