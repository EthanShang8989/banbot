package orm

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLiteMigration(t *testing.T) {
	// 创建临时数据库文件
	tmpDir := os.TempDir()
	dbPath := filepath.Join(tmpDir, "test_migration.db")
	defer os.Remove(dbPath)

	// 打开数据库连接
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_journal_mode=WAL&_sync=NORMAL&_foreign_keys=on&_busy_timeout=8000")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// 创建旧版本的exorder表（没有fee_quote字段）
	oldTableSQL := `
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
	)`

	_, err = db.Exec(oldTableSQL)
	if err != nil {
		t.Fatalf("Failed to create old table: %v", err)
	}

	// 插入测试数据
	insertSQL := `
	INSERT INTO exorder (
		task_id, inout_id, symbol, enter, order_type, order_id, side,
		create_at, price, average, amount, filled, status, fee, fee_type, update_at
	) VALUES (1, 1, 'BTCUSDT', 1, 'LIMIT', 'order123', 'BUY', ?, 50000.0, 50000.0, 1.0, 1.0, 1, 10.0, 'USDT', ?)`

	now := time.Now().UnixMilli()
	_, err = db.Exec(insertSQL, now, now)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// 验证fee_quote字段不存在
	rows, err := db.Query("PRAGMA table_info(exorder)")
	if err != nil {
		t.Fatalf("Failed to get table info: %v", err)
	}
	
	hasColumnFeeQuote := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue *string // 使用指针类型处理NULL值
		err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk)
		if err != nil {
			t.Fatalf("Failed to scan table info: %v", err)
		}
		if name == "fee_quote" {
			hasColumnFeeQuote = true
		}
	}
	rows.Close()

	if hasColumnFeeQuote {
		t.Fatal("fee_quote column should not exist before migration")
	}

	// 执行迁移
	migrationErr := runSQLiteMigrations(db)
	if migrationErr != nil {
		t.Fatalf("Migration failed: %v", migrationErr)
	}

	// 验证fee_quote字段已添加
	rows, err = db.Query("PRAGMA table_info(exorder)")
	if err != nil {
		t.Fatalf("Failed to get table info after migration: %v", err)
	}
	
	hasColumnFeeQuoteAfter := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue *string // 使用指针类型处理NULL值
		err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk)
		if err != nil {
			t.Fatalf("Failed to scan table info after migration: %v", err)
		}
		if name == "fee_quote" {
			hasColumnFeeQuoteAfter = true
		}
	}
	rows.Close()

	if !hasColumnFeeQuoteAfter {
		t.Fatal("fee_quote column should exist after migration")
	}

	// 验证数据是否保持完整
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM exorder").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count records: %v", err)
	}
	if count != 1 {
		t.Fatalf("Expected 1 record, got %d", count)
	}

	// 验证新字段的默认值
	var feeQuote float64
	err = db.QueryRow("SELECT fee_quote FROM exorder WHERE id = 1").Scan(&feeQuote)
	if err != nil {
		t.Fatalf("Failed to select fee_quote: %v", err)
	}
	if feeQuote != 0.0 {
		t.Fatalf("Expected fee_quote to be 0.0, got %f", feeQuote)
	}

	t.Log("✅ SQLite migration test passed")
}

func TestSQLiteMigrationIdempotent(t *testing.T) {
	// 创建临时数据库文件
	tmpDir := os.TempDir()
	dbPath := filepath.Join(tmpDir, "test_migration_idempotent.db")
	defer os.Remove(dbPath)

	// 打开数据库连接
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_journal_mode=WAL&_sync=NORMAL&_foreign_keys=on&_busy_timeout=8000")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// 创建已经包含fee_quote字段的表
	newTableSQL := `
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
		fee_quote  REAL    NOT NULL DEFAULT 0.0,
		fee_type   TEXT    NOT NULL,
		update_at  INTEGER NOT NULL
	)`

	_, err = db.Exec(newTableSQL)
	if err != nil {
		t.Fatalf("Failed to create new table: %v", err)
	}

	// 多次执行迁移应该是安全的
	for i := 0; i < 3; i++ {
		migrationErr := runSQLiteMigrations(db)
		if migrationErr != nil {
			t.Fatalf("Migration failed on iteration %d: %v", i+1, migrationErr)
		}
	}

	t.Log("✅ SQLite idempotent migration test passed")
}