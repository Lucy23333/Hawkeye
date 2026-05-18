package database

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

func InitDB() {
	dsn := "root:root@tcp(127.0.0.1:3306)/hawkeye?parseTime=true&loc=Local"
	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		fmt.Println(" DB Error:", err)
		return
	}
	if err = DB.Ping(); err != nil {
		fmt.Println(" DB Connect Fail:", err)
		return
	}
	// 自动建表，防止首次运行报错
	DB.Exec("CREATE TABLE IF NOT EXISTS events (id INT AUTO_INCREMENT PRIMARY KEY, filename VARCHAR(255), capture_time DATETIME, ai_analysis TEXT, device_id VARCHAR(50) DEFAULT 'CAM-01')")
	DB.Exec("CREATE TABLE IF NOT EXISTS devices (id VARCHAR(50) PRIMARY KEY, last_seen DATETIME, enabled TINYINT(1) DEFAULT 1)")
	DB.Exec("ALTER TABLE events ADD COLUMN status VARCHAR(20) DEFAULT 'open'")
	DB.Exec("ALTER TABLE events ADD COLUMN tags VARCHAR(255) DEFAULT ''")
	DB.Exec("INSERT INTO devices (id, last_seen, enabled) SELECT device_id, MAX(capture_time), 1 FROM events GROUP BY device_id ON DUPLICATE KEY UPDATE last_seen=VALUES(last_seen)")
	fmt.Println(" Database Schema Ready")
}