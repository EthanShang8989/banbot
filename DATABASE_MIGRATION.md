# 数据库自动迁移功能
Database Auto Migration Feature

## 问题描述 Problem Description

服务器运行时遇到 `fee_quote` 字段缺失错误：
```
SQL logic error: table exorder has no column named fee_quote
```

这是因为代码中新增了 `fee_quote` 字段，但数据库架构还是旧版本。

## 解决方案 Solution

已实现**自动数据库迁移功能**，当你执行 `banbot spider` 或任何其他命令时，系统会自动：

1. **检测数据库架构版本**
2. **自动添加缺失的字段**
3. **保持数据完整性**
4. **无需手动干预**

## 实现详情 Implementation Details

### 文件更改 File Changes

1. **`orm/sqlite_migrations.go`** - SQLite自动迁移功能
2. **`orm/sqlite_migrations_test.go`** - 迁移功能测试
3. **`orm/base.go`** - 在数据库初始化时调用迁移
4. **`orm/sql/pg_migrations.sql`** - PostgreSQL迁移脚本（version 3）

### 迁移逻辑 Migration Logic

```go
// 检查字段是否存在
PRAGMA table_info(exorder);

// 如果不存在，添加字段
ALTER TABLE exorder ADD COLUMN fee_quote REAL NOT NULL DEFAULT 0.0;

// 如果ALTER失败，重建表结构
```

### 安全特性 Safety Features

- ✅ **幂等性**：多次执行迁移是安全的
- ✅ **事务性**：失败时自动回滚
- ✅ **数据保护**：现有数据不会丢失
- ✅ **默认值**：新字段设置合理默认值（0.0）
- ✅ **索引重建**：自动重建必要的索引

## 使用方法 Usage

### 立即修复服务器问题 Immediate Fix

```bash
# 1. 停止服务
systemctl stop ethlparblxh.service

# 2. 备份数据库（重要！）
cp $BanDataDir/trades.db $BanDataDir/trades.db.backup.$(date +%Y%m%d_%H%M%S)

# 3. 更新程序到最新版本
# 替换banbot二进制文件

# 4. 启动任何命令触发自动迁移
cd /path/to/banbot
./banbot spider -config config.yml  # 或任何其他命令

# 5. 重启服务
systemctl start ethlparblxh.service

# 6. 检查日志确认无错误
journalctl -u ethlparblxh.service -f
```

### 验证迁移成功 Verify Migration Success

```bash
# 检查表结构，应该看到 fee_quote 字段
sqlite3 $BanDataDir/trades.db "PRAGMA table_info(exorder);"

# 检查数据完整性
sqlite3 $BanDataDir/trades.db "SELECT COUNT(*) FROM exorder;"
```

## 测试验证 Testing

运行迁移测试：
```bash
go test -v -run TestSQLiteMigration ./orm
```

运行完整演示：
```bash
./test_db_migration.sh
```

## 日志输出 Log Output

成功迁移时的日志：
```
INFO orm/sqlite_migrations.go:30 检查SQLite数据库迁移...
INFO orm/sqlite_migrations.go:68 添加缺失的fee_quote字段到exorder表...
INFO orm/sqlite_migrations.go:92 ✅ fee_quote字段添加成功
INFO orm/sqlite_migrations.go:37 SQLite数据库迁移完成
```

字段已存在时的日志：
```
INFO orm/sqlite_migrations.go:30 检查SQLite数据库迁移...
INFO orm/sqlite_migrations.go:94 fee_quote字段已存在，跳过迁移
INFO orm/sqlite_migrations.go:37 SQLite数据库迁移完成
```

## 错误处理 Error Handling

如果迁移失败，系统会：
1. 尝试 `ALTER TABLE` 方式添加字段
2. 如果失败，自动重建表结构
3. 保持原始数据完整性
4. 记录详细错误日志

## 支持的数据库 Supported Databases

- ✅ **SQLite** - 完整自动迁移支持
- ✅ **PostgreSQL** - 通过 `pg_migrations.sql` version 3

## 未来扩展 Future Extensions

迁移系统设计为可扩展的，新的架构更改只需：
1. 在 `runSQLiteMigrations()` 中添加检查逻辑
2. 创建相应的迁移函数
3. 添加测试验证

## 注意事项 Notes

- 迁移在每次数据库连接时检查（仅检查一次）
- 对性能影响极小（毫秒级检查）
- 完全向后兼容
- 无需修改现有配置文件

---

**总结**：这个自动迁移功能彻底解决了 `fee_quote` 字段缺失问题，并为未来的数据库架构升级提供了健壮的基础。