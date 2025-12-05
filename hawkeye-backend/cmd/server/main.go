package main

import (
	"fmt"
	"hawkeye/internal/config"
	"hawkeye/internal/database"
	"hawkeye/internal/handlers"
	"hawkeye/web" 
	"net/http"
	"os"
)


func main() {
	// 1. 初始化
	config.InitConfig()
	os.MkdirAll("./uploads/avatars", 0755)
	database.InitDB()

	// 2. 注入模板 (使用 web 包里的 Content)
	handlers.SetTemplates(web.Content) 

	// 3. 注册路由
	http.HandleFunc("/", handlers.SplashHandler)
	http.HandleFunc("/app", handlers.AuthMiddleware(handlers.AppHandler))
	http.HandleFunc("/login", handlers.LoginHandler)
	http.HandleFunc("/logout", handlers.LogoutHandler)
	http.HandleFunc("/camera", handlers.CameraHandler)

	http.HandleFunc("/api/stream", handlers.StreamHandler)
	http.HandleFunc("/api/events", handlers.AuthMiddleware(handlers.EventsAPIHandler))
	http.HandleFunc("/api/devices", handlers.AuthMiddleware(handlers.DevicesAPIHandler))
	http.HandleFunc("/settings", handlers.AuthMiddleware(handlers.SettingsHandler))
	http.HandleFunc("/api/upload_avatar", handlers.AuthMiddleware(handlers.AvatarUploadHandler))
	http.HandleFunc("/api/update_profile", handlers.AuthMiddleware(handlers.UpdateProfileHandler))
	http.HandleFunc("/delete_device", handlers.AuthMiddleware(handlers.DeleteDeviceHandler))
	http.HandleFunc("/analyze", handlers.AuthMiddleware(handlers.AnalyzeHandler))
	http.HandleFunc("/delete", handlers.AuthMiddleware(handlers.DeleteHandler))
	
	http.HandleFunc("/upload", handlers.UploadHandler)
	
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("./uploads"))))

	fmt.Println("🦅 鹰眼 已启动 http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}