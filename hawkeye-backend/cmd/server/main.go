package main

import (
	"fmt"
	"hawkeye/internal/config"
	"hawkeye/internal/database"
	"hawkeye/internal/handlers"
	"hawkeye/internal/storage"
	"hawkeye/web"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	// 1. 初始化
	config.InitConfig()
	if err := storage.EnsureUploadDirs(); err != nil {
		log.Fatal(err)
	}
	database.InitDB()

	// 2. 注入模板 (使用 web 包里的 Content)
	handlers.SetTemplates(web.Content)

	// 启动后台 Worker 协程
	go handlers.StartWorker()
	go handlers.StartRetentionWorker()
	handlers.RequeuePendingAnalyses()

	// 3. 注册路由
	http.HandleFunc("/", handlers.SplashHandler)
	http.HandleFunc("/app", handlers.AuthMiddleware(handlers.AppHandler))
	http.HandleFunc("/login", handlers.LoginHandler)
	http.HandleFunc("/logout", handlers.LogoutHandler)
	http.HandleFunc("/camera", handlers.CameraHandler)
	http.HandleFunc("/health", handlers.HealthHandler)

	http.HandleFunc("/api/stream", handlers.StreamHandler)
	// SSE 实时警报流
	http.HandleFunc("/api/events/subscribe", handlers.AlertSubscribeHandler)

	http.HandleFunc("/api/events", handlers.AuthMiddleware(handlers.EventsAPIHandler))
	http.HandleFunc("/api/events/update", handlers.AuthMiddleware(handlers.EventsUpdateHandler))
	http.HandleFunc("/api/queue", handlers.AuthMiddleware(handlers.QueueStatusHandler))
	http.HandleFunc("/api/devices", handlers.AuthMiddleware(handlers.DevicesAPIHandler))
	http.HandleFunc("/settings", handlers.AuthMiddleware(handlers.SettingsHandler))
	http.HandleFunc("/api/upload_avatar", handlers.AuthMiddleware(handlers.AvatarUploadHandler))
	http.HandleFunc("/api/update_profile", handlers.AuthMiddleware(handlers.UpdateProfileHandler))
	http.HandleFunc("/delete_device", handlers.AuthMiddleware(handlers.DeleteDeviceHandler))
	http.HandleFunc("/analyze", handlers.AuthMiddleware(handlers.AnalyzeHandler))
	http.HandleFunc("/delete", handlers.AuthMiddleware(handlers.DeleteHandler))

	http.HandleFunc("/upload", handlers.UploadHandler)

	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("./uploads"))))

	addr := os.Getenv("HAWKEYE_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	fmt.Println("鹰眼 已启动 http://localhost" + addr)
	srv := &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}
