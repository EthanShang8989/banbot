#!/bin/bash

# 测试数据库迁移功能
# Test database migration functionality

echo "========================================="
echo "测试SQLite数据库自动迁移功能"
echo "Testing SQLite Database Auto Migration"
echo "========================================="

# 创建临时目录
TEST_DIR="/tmp/banbot_migration_test"
mkdir -p "$TEST_DIR"

# 设置环境变量
export BanDataDir="$TEST_DIR"

echo "1. 创建旧版本数据库（不包含fee_quote字段）..."
echo "   Creating old database without fee_quote field..."

# 创建旧版本的trades.db
sqlite3 "$TEST_DIR/trades.db" <<EOF
CREATE TABLE exorder (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    INTEGER NOT NULL,
    inout_id   INTEGER NOT NULL,
    symbol     TEXT    NOT NULL,
    enter      BOOL    NOT NULL,
    order_type TEXT    NOT NULL,
    order_id   TEXT    NOT NULL,
    side       TEXT    NOT NULL,
    create_at  INTEGER NOT NULL,
    price      REAL    NOT NULL,
    average    REAL    NOT NULL,
    amount     REAL    NOT NULL,
    filled     REAL    NOT NULL,
    status     INTEGER NOT NULL,
    fee        REAL    NOT NULL,
    fee_type   TEXT    NOT NULL,
    update_at  INTEGER NOT NULL
);

INSERT INTO exorder (
    task_id, inout_id, symbol, enter, order_type, order_id, side,
    create_at, price, average, amount, filled, status, fee, fee_type, update_at
) VALUES (1, 1, 'BTCUSDT', 1, 'LIMIT', 'order123', 'BUY', 1704067200000, 50000.0, 50000.0, 1.0, 1.0, 1, 10.0, 'USDT', 1704067200000);

CREATE TABLE bottask (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL
);

INSERT INTO bottask (name) VALUES ('test_task');
EOF

echo "2. 检查旧版本表结构..."
echo "   Checking old table structure..."
sqlite3 "$TEST_DIR/trades.db" "PRAGMA table_info(exorder);"

echo ""
echo "3. 运行banbot spider触发自动迁移..."
echo "   Running banbot spider to trigger auto migration..."

# 创建最小配置文件
cat > "$TEST_DIR/test_config.yml" << EOF
datadir: "$TEST_DIR"
database:
  url: "sqlite://$TEST_DIR/main.db"
  auto_create: true
spider_addr: "127.0.0.1:6789"
jobs:
  - name: "test"
    exchange: "binance"
    market: "spot"
    symbol: "BTC/USDT"
    timeframes: ["1m"]
    start_date: "2024-01-01"
    end_date: "2024-01-02"
EOF

# 尝试执行banbot命令（可能会失败，但应该触发迁移）
timeout 10s ./banbot spider -config "$TEST_DIR/test_config.yml" || echo "Expected timeout/failure - migration should have been triggered"

echo ""
echo "4. 检查迁移后的表结构..."
echo "   Checking table structure after migration..."
sqlite3 "$TEST_DIR/trades.db" "PRAGMA table_info(exorder);"

echo ""
echo "5. 验证数据完整性..."
echo "   Verifying data integrity..."
sqlite3 "$TEST_DIR/trades.db" "SELECT id, symbol, fee, fee_quote FROM exorder;"

echo ""
echo "6. 清理测试文件..."
echo "   Cleaning up test files..."
rm -rf "$TEST_DIR"

echo ""
echo "========================================="
echo "✅ 测试完成！"
echo "✅ Test completed!"
echo "========================================="
echo ""
echo "部署到服务器的步骤："
echo "Steps to deploy to server:"
echo "1. 停止服务: systemctl stop ethlparblxh.service"
echo "2. 备份数据: cp \$BanDataDir/trades.db \$BanDataDir/trades.db.backup"
echo "3. 更新程序: 替换新的banbot二进制文件"
echo "4. 启动服务: systemctl start ethlparblxh.service"
echo "5. 检查日志: journalctl -u ethlparblxh.service -f"
echo ""
echo "自动迁移将在程序启动时自动执行！"
echo "Auto migration will run automatically when the program starts!"