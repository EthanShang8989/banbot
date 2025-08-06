#!/bin/bash

echo "检查 Binance 聚合交易数据格式"
echo "================================"

# 创建临时目录
TEMP_DIR="/tmp/format_check"
mkdir -p $TEMP_DIR

# 下载一个小的样本文件
echo "下载样本数据..."
curl -s "https://data.binance.vision/data/spot/daily/aggTrades/BTCUSDT/BTCUSDT-aggTrades-2024-01-01.zip" -o $TEMP_DIR/sample.zip

if [ $? -ne 0 ]; then
    echo "下载失败!"
    exit 1
fi

# 解压文件
echo "解压文件..."
cd $TEMP_DIR
unzip -q sample.zip

# 查看前5行
echo ""
echo "CSV 文件前5行："
echo "--------------------------------"
head -5 *.csv

echo ""
echo "字段数量检查："
echo "--------------------------------"
head -1 *.csv | awk -F',' '{print "字段数量: " NF}'

echo ""
echo "第一行详细分析："
echo "--------------------------------"
head -1 *.csv | awk -F',' '{
    print "Field 1 (AggTradeID): " $1
    print "Field 2 (Price): " $2  
    print "Field 3 (Quantity): " $3
    print "Field 4 (FirstTradeID): " $4
    print "Field 5 (LastTradeID): " $5
    print "Field 6 (Timestamp): " $6
    print "Field 7 (IsBuyerMaker): " $7
    if (NF >= 8) print "Field 8 (Extra): " $8
}'

# 清理
rm -rf $TEMP_DIR

echo ""
echo "检查完成!"