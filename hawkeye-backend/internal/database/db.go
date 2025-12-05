package database

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

func InitDB() {
	dsn := "root:******@tcp(127.0.0.1:3306)/hawkeye?parseTime=true&loc=Local"
	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		fmt.Println("❌ DB Error:", err)
		return
	}
	if err = DB.Ping(); err != nil {
		fmt.Println("❌ DB Connect Fail:", err)
		return
	}
	// 自动建表，防止首次运行报错
	DB.Exec("CREATE TABLE IF NOT EXISTS events (id INT AUTO_INCREMENT PRIMARY KEY, filename VARCHAR(255), capture_time DATETIME, ai_analysis TEXT, device_id VARCHAR(50) DEFAULT 'CAM-01')")
	fmt.Println("✅ Database Schema Ready")
}