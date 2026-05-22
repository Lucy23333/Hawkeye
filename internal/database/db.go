package database

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"os"
	"strings"
	"time"
)

var DB *sql.DB

func InitDB() error {
	dsn := getDSN()
	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(10)
	DB.SetConnMaxLifetime(30 * time.Minute)
	if err = DB.Ping(); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	// 自动建表，防止首次运行报错
	if err := mustExec("CREATE TABLE IF NOT EXISTS events (id INT AUTO_INCREMENT PRIMARY KEY, filename VARCHAR(255) NOT NULL, capture_time DATETIME NOT NULL, ai_analysis TEXT, device_id VARCHAR(50) DEFAULT 'CAM-01')"); err != nil {
		return err
	}
	if err := mustExec("CREATE TABLE IF NOT EXISTS devices (id VARCHAR(50) PRIMARY KEY, last_seen DATETIME, enabled TINYINT(1) DEFAULT 1)"); err != nil {
		return err
	}
	if err := ensureColumn("events", "status", "ALTER TABLE events ADD COLUMN status VARCHAR(20) DEFAULT 'open'"); err != nil {
		return err
	}
	if err := ensureColumn("events", "tags", "ALTER TABLE events ADD COLUMN tags VARCHAR(255) DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn("events", "analysis_status", "ALTER TABLE events ADD COLUMN analysis_status VARCHAR(20) DEFAULT 'pending'"); err != nil {
		return err
	}
	if err := ensureColumn("events", "analysis_error", "ALTER TABLE events ADD COLUMN analysis_error TEXT"); err != nil {
		return err
	}
	if err := ensureColumn("events", "analysis_attempts", "ALTER TABLE events ADD COLUMN analysis_attempts INT DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureColumn("events", "created_at", "ALTER TABLE events ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP"); err != nil {
		return err
	}
	if err := ensureColumn("events", "updated_at", "ALTER TABLE events ADD COLUMN updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP"); err != nil {
		return err
	}
	if err := ensureColumn("devices", "created_at", "ALTER TABLE devices ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP"); err != nil {
		return err
	}
	if err := ensureIndex("events", "idx_events_capture_time", "ALTER TABLE events ADD INDEX idx_events_capture_time (capture_time)"); err != nil {
		return err
	}
	if err := ensureIndex("events", "idx_events_device_status", "ALTER TABLE events ADD INDEX idx_events_device_status (device_id, status)"); err != nil {
		return err
	}
	if err := ensureIndex("events", "uniq_events_filename", "ALTER TABLE events ADD UNIQUE KEY uniq_events_filename (filename)"); err != nil {
		return err
	}
	if err := mustExec("INSERT INTO devices (id, last_seen, enabled) SELECT device_id, MAX(capture_time), 1 FROM events GROUP BY device_id ON DUPLICATE KEY UPDATE last_seen=VALUES(last_seen)"); err != nil {
		return err
	}
	fmt.Println(" Database Schema Ready")
	return nil
}

func getDSN() string {
	if dsn := os.Getenv("HAWKEYE_DSN"); dsn != "" {
		return dsn
	}
	if dsn := os.Getenv("DB_DSN"); dsn != "" {
		return dsn
	}
	return "root:root@tcp(127.0.0.1:3306)/hawkeye?parseTime=true&loc=Local"
}

func mustExec(query string, args ...interface{}) error {
	if _, err := DB.Exec(query, args...); err != nil {
		return fmt.Errorf("exec schema query: %w", err)
	}
	return nil
}

func ensureColumn(table string, column string, alterSQL string) error {
	var count int
	err := DB.QueryRow(
		"SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?",
		table,
		column,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("check column %s.%s: %w", table, column, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := DB.Exec(alterSQL); err != nil && !strings.Contains(err.Error(), "Duplicate column") {
		return fmt.Errorf("alter column %s.%s: %w", table, column, err)
	}
	return nil
}

func ensureIndex(table string, index string, alterSQL string) error {
	var count int
	err := DB.QueryRow(
		"SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?",
		table,
		index,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("check index %s.%s: %w", table, index, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := DB.Exec(alterSQL); err != nil {
		if strings.Contains(err.Error(), "Duplicate key name") {
			return nil
		}
		return fmt.Errorf("alter index %s.%s: %w", table, index, err)
	}
	return nil
}
