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

func InitDB() {
	dsn := getDSN()
	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		fmt.Println(" DB Error:", err)
		return
	}
	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(10)
	DB.SetConnMaxLifetime(30 * time.Minute)
	if err = DB.Ping(); err != nil {
		fmt.Println(" DB Connect Fail:", err)
		return
	}
	// 自动建表，防止首次运行报错
	mustExec("CREATE TABLE IF NOT EXISTS events (id INT AUTO_INCREMENT PRIMARY KEY, filename VARCHAR(255), capture_time DATETIME, ai_analysis TEXT, device_id VARCHAR(50) DEFAULT 'CAM-01')")
	mustExec("CREATE TABLE IF NOT EXISTS devices (id VARCHAR(50) PRIMARY KEY, last_seen DATETIME, enabled TINYINT(1) DEFAULT 1)")
	ensureColumn("events", "status", "ALTER TABLE events ADD COLUMN status VARCHAR(20) DEFAULT 'open'")
	ensureColumn("events", "tags", "ALTER TABLE events ADD COLUMN tags VARCHAR(255) DEFAULT ''")
	ensureColumn("events", "analysis_status", "ALTER TABLE events ADD COLUMN analysis_status VARCHAR(20) DEFAULT 'pending'")
	ensureColumn("events", "analysis_error", "ALTER TABLE events ADD COLUMN analysis_error TEXT")
	ensureColumn("events", "analysis_attempts", "ALTER TABLE events ADD COLUMN analysis_attempts INT DEFAULT 0")
	ensureColumn("events", "created_at", "ALTER TABLE events ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP")
	ensureColumn("devices", "created_at", "ALTER TABLE devices ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP")
	mustExec("INSERT INTO devices (id, last_seen, enabled) SELECT device_id, MAX(capture_time), 1 FROM events GROUP BY device_id ON DUPLICATE KEY UPDATE last_seen=VALUES(last_seen)")
	fmt.Println(" Database Schema Ready")
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

func mustExec(query string, args ...interface{}) {
	if _, err := DB.Exec(query, args...); err != nil {
		fmt.Println(" DB Exec Fail:", err)
	}
}

func ensureColumn(table string, column string, alterSQL string) {
	var count int
	err := DB.QueryRow(
		"SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?",
		table,
		column,
	).Scan(&count)
	if err != nil {
		fmt.Println(" DB Schema Check Fail:", err)
		return
	}
	if count > 0 {
		return
	}
	if _, err := DB.Exec(alterSQL); err != nil && !strings.Contains(err.Error(), "Duplicate column") {
		fmt.Println(" DB Alter Fail:", err)
	}
}
