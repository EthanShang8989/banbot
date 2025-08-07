package orm

import (
	"database/sql"

	"github.com/banbox/banbot/core"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

// SQLite迁移脚本版本管理
const sqliteMigrationsSQL = `
-- version 1
-- 添加fee_quote字段到exorder表
-- Add fee_quote field to exorder table

-- 检查字段是否已存在
-- Check if the field already exists
PRAGMA table_info(exorder);

-- 如果字段不存在，添加它
-- If the field doesn't exist, add it
-- Note: SQLite doesn't support IF NOT EXISTS for ALTER TABLE ADD COLUMN
-- We'll use a different approach in the code
`

// runSQLiteMigrations 执行SQLite数据库迁移
func runSQLiteMigrations(db *sql.DB) *errs.Error {
	log.Info("检查SQLite数据库迁移...")
	
	// 1. 检查exorder表是否存在fee_quote字段
	if err := checkAndAddFeeQuoteColumn(db); err != nil {
		return err
	}
	
	log.Info("SQLite数据库迁移完成")
	return nil
}

// checkAndAddFeeQuoteColumn 检查并添加fee_quote字段
func checkAndAddFeeQuoteColumn(db *sql.DB) *errs.Error {
	// 检查exorder表结构
	rows, err := db.Query("PRAGMA table_info(exorder)")
	if err != nil {
		return errs.New(core.ErrDbReadFail, err)
	}
	defer rows.Close()
	
	hasColumnFeeQuote := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue *string // 使用指针类型处理NULL值
		err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk)
		if err != nil {
			return errs.New(core.ErrDbReadFail, err)
		}
		if name == "fee_quote" {
			hasColumnFeeQuote = true
			break
		}
	}
	
	// 如果字段不存在，添加它
	if !hasColumnFeeQuote {
		log.Info("添加缺失的fee_quote字段到exorder表...")
		
		// 使用事务确保操作的原子性
		tx, err := db.Begin()
		if err != nil {
			return errs.New(core.ErrDbExecFail, err)
		}
		defer tx.Rollback()
		
		// 添加fee_quote字段
		_, err = tx.Exec("ALTER TABLE exorder ADD COLUMN fee_quote REAL NOT NULL DEFAULT 0.0")
		if err != nil {
			// 如果ALTER TABLE失败，可能需要重建表
			log.Warn("ALTER TABLE失败，尝试重建表", zap.Error(err))
			if rebuildErr := rebuildExorderTable(tx); rebuildErr != nil {
				return rebuildErr
			}
		}
		
		// 提交事务
		if err = tx.Commit(); err != nil {
			return errs.New(core.ErrDbExecFail, err)
		}
		
		log.Info("✅ fee_quote字段添加成功")
	} else {
		log.Info("fee_quote字段已存在，跳过迁移")
	}
	
	return nil
}

// rebuildExorderTable 重建exorder表（包含fee_quote字段）
func rebuildExorderTable(tx *sql.Tx) *errs.Error {
	log.Info("重建exorder表结构...")
	
	// 1. 创建新表
	createTableSQL := `
	CREATE TABLE exorder_new (
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
	
	_, err := tx.Exec(createTableSQL)
	if err != nil {
		return errs.New(core.ErrDbExecFail, err)
	}
	
	// 2. 复制现有数据（为新字段设置默认值）
	copyDataSQL := `
	INSERT INTO exorder_new (
		id, task_id, inout_id, symbol, enter, order_type, order_id, side,
		create_at, price, average, amount, filled, status, fee, fee_quote, fee_type, update_at
	)
	SELECT 
		id, task_id, inout_id, symbol, enter, order_type, order_id, side,
		create_at, price, average, amount, filled, status, fee, 0.0, fee_type, update_at
	FROM exorder`
	
	_, err = tx.Exec(copyDataSQL)
	if err != nil {
		return errs.New(core.ErrDbExecFail, err)
	}
	
	// 3. 删除旧表
	_, err = tx.Exec("DROP TABLE exorder")
	if err != nil {
		return errs.New(core.ErrDbExecFail, err)
	}
	
	// 4. 重命名新表
	_, err = tx.Exec("ALTER TABLE exorder_new RENAME TO exorder")
	if err != nil {
		return errs.New(core.ErrDbExecFail, err)
	}
	
	// 5. 重建索引
	indexSQL := []string{
		"CREATE INDEX idx_od_inout_id ON exorder (inout_id)",
		"CREATE INDEX idx_od_status   ON exorder (status)",
		"CREATE INDEX idx_od_task_id  ON exorder (task_id)",
	}
	
	for _, sql := range indexSQL {
		_, err = tx.Exec(sql)
		if err != nil {
			log.Warn("创建索引失败", zap.String("sql", sql), zap.Error(err))
			// 索引创建失败不是致命错误，继续执行
		}
	}
	
	log.Info("✅ exorder表重建完成")
	return nil
}