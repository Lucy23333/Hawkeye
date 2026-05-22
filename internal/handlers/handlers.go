package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hawkeye/internal/config"
	"hawkeye/internal/database"
	"hawkeye/internal/models"
	"hawkeye/internal/security"
	"hawkeye/internal/storage"
	"hawkeye/internal/stream"
	"html/template"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type AnalysisJob struct {
	Filename string
	DeviceID string
}

const (
	analysisMaxAttempts = 3
	analysisLeaseTTL    = 2 * time.Minute
	analysisHTTPTimeout = 25 * time.Second
)

type sessionInfo struct {
	CSRF    string
	Expires time.Time
}

const (
	sessionCookieName = "session"
	csrfCookieName    = "csrf"
	sessionTTL        = 24 * time.Hour
)

var (
	sessionMu sync.RWMutex
	sessions  = map[string]sessionInfo{}
)

var serverStart = time.Now()
var jobStats = struct {
	sync.Mutex
	Processed int
	Failed    int
	Active    int
}{}

var analysisWake = make(chan struct{}, 1)

type rateWindow struct {
	start time.Time
	count int
}

type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	items  map[string]*rateWindow
}

var uploadLimiter = newRateLimiter(60, time.Minute)

// 模板系统
var templates embed.FS

func SetTemplates(fs embed.FS) {
	templates = fs
}

func renderTemplate(w http.ResponseWriter, tmplName string, data interface{}) {
	t, err := template.ParseFS(templates, "templates/"+tmplName)
	if err != nil {
		http.Error(w, "Template Error: "+err.Error(), 500)
		return
	}
	if err := t.Execute(w, data); err != nil {
		log.Println("template execute failed:", err)
	}
}

//页面 Handlers

func SplashHandler(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "splash.html", nil)
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		renderTemplate(w, "login.html", nil)
		return
	}
	var c struct {
		Username string
		Password string
	}
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "Bad Request", 400)
		return
	}
	config.ConfigMu.RLock()
	u := config.AppConfig.AdminUser
	p := config.AppConfig.AdminPass
	config.ConfigMu.RUnlock()
	if c.Username == u && security.VerifyPassword(p, c.Password) {
		sessionToken := newToken(32)
		csrfToken := newToken(16)
		sessionMu.Lock()
		sessions[sessionToken] = sessionInfo{CSRF: csrfToken, Expires: time.Now().Add(sessionTTL)}
		sessionMu.Unlock()
		setSessionCookies(w, r, sessionToken, csrfToken)
		fmt.Fprint(w, "OK")
	} else {
		http.Error(w, "Fail", 401)
	}
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		sessionMu.Lock()
		delete(sessions, c.Value)
		sessionMu.Unlock()
	}
	clearSessionCookies(w, r)
	http.Redirect(w, r, "/login", 302)
}

func AppHandler(w http.ResponseWriter, r *http.Request) {
	config.ConfigMu.RLock()
	defer config.ConfigMu.RUnlock()
	renderTemplate(w, "app.html", config.AppConfig)
}

func CameraHandler(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "camera.html", nil)
}

//中间件

func AuthMiddleware(n http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := getSession(r)
		if !ok {
			http.Redirect(w, r, "/login", 302)
			return
		}
		n(w, r)
	}
}

//API Handlers

func StreamHandler(w http.ResponseWriter, r *http.Request) {
	if !requireDeviceKey(w, r) {
		return
	}
	devID := r.URL.Query().Get("device_id")
	if devID == "" {
		devID = "CAM-01"
	}
	if !isSafeDeviceID(devID) {
		http.Error(w, "Invalid device_id", 400)
		return
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	ch := stream.AddViewer(devID)
	defer stream.RemoveViewer(devID, ch)

	for imgData := range ch {
		fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(imgData))
		w.Write(imgData)
		w.Write([]byte("\r\n"))
	}
}

func AlertSubscribeHandler(w http.ResponseWriter, r *http.Request) {
	if !requireDeviceKey(w, r) {
		return
	}
	stream.AlertBroker.ServeHTTP(w, r)
}

func UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		return
	}
	if !requireDeviceKey(w, r) {
		return
	}
	if !uploadLimiter.Allow(getClientIP(r)) {
		http.Error(w, "Too Many Requests", 429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "Payload too large", 413)
		return
	}
	file, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Missing image", 400)
		return
	}
	defer file.Close()
	imgData, err := ioutil.ReadAll(io.LimitReader(file, 10<<20))
	if err != nil {
		http.Error(w, "Read image failed", 400)
		return
	}
	if !isSupportedImage(imgData) {
		http.Error(w, "Unsupported image", 415)
		return
	}
	deviceID := r.FormValue("device_id")
	if deviceID == "" {
		deviceID = "CAM-01"
	}
	if !isSafeDeviceID(deviceID) {
		http.Error(w, "Invalid device_id", 400)
		return
	}
	mode := r.FormValue("mode")

	var enabled int
	if err := database.DB.QueryRow("SELECT enabled FROM devices WHERE id=?", deviceID).Scan(&enabled); err == nil && enabled == 0 {
		http.Error(w, "Device disabled", 403)
		return
	}

	now := time.Now().In(models.CstZone)
	if _, err := database.DB.Exec("INSERT INTO devices (id, last_seen, enabled) VALUES (?,?,1) ON DUPLICATE KEY UPDATE last_seen=VALUES(last_seen)", deviceID, now); err != nil {
		http.Error(w, "Device update failed", 500)
		log.Println("device upsert failed:", err)
		return
	}

	// 1. 广播流
	stream.BroadcastFrame(deviceID, imgData)

	// 2. 纯流模式，不保存
	if mode == "stream" {
		return
	}

	// 3. 抓拍模式：保存到硬盘和数据库
	n := fmt.Sprintf("%s_%s_evidence.jpg", now.Format("20060102-150405.000000000"), security.NewToken(4))
	if err := storage.SaveImage(n, imgData); err != nil {
		http.Error(w, "Write image failed", 500)
		log.Println("image write failed:", err)
		return
	}
	if _, err := database.DB.Exec("INSERT INTO events (filename, capture_time, device_id, analysis_status) VALUES (?,?,?,'queued')", n, now, deviceID); err != nil {
		http.Error(w, "Event insert failed", 500)
		log.Println("event insert failed:", err)
		_ = storage.RemoveImage(n)
		return
	}

	// 🔥 新增：自动触发 AI 分析
	// 只有非流模式(stream)才分析，避免浪费 Token
	if mode != "stream" {
		wakeAnalysisWorkers()
		log.Println("analysis queued:", n)
	}

	fmt.Fprintf(w, "OK")
}

func EventsAPIHandler(w http.ResponseWriter, r *http.Request) {
	query := "SELECT id, filename, capture_time, IFNULL(ai_analysis, ''), IFNULL(device_id, 'CAM-01'), IFNULL(status, 'open'), IFNULL(tags, ''), IFNULL(analysis_status, 'pending'), IFNULL(analysis_error, ''), IFNULL(analysis_attempts, 0) FROM events WHERE 1=1"
	args := []interface{}{}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		query += " AND status=?"
		args = append(args, status)
	}
	if deviceID := strings.TrimSpace(r.URL.Query().Get("device_id")); deviceID != "" {
		query += " AND device_id=?"
		args = append(args, deviceID)
	}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		query += " AND (filename LIKE ? OR ai_analysis LIKE ? OR tags LIKE ?)"
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	query += " ORDER BY id DESC LIMIT 100"
	rows, err := database.DB.Query(query, args...)
	if err != nil {
		http.Error(w, "Query failed", 500)
		log.Println("events query failed:", err)
		return
	}
	defer rows.Close()
	var events []models.Event
	for rows.Next() {
		var e models.Event
		var t time.Time
		if err := rows.Scan(&e.ID, &e.Filename, &t, &e.AIAnalysis, &e.DeviceID, &e.Status, &e.Tags, &e.AnalysisStatus, &e.AnalysisError, &e.AnalysisAttempts); err != nil {
			log.Println("event scan failed:", err)
			continue
		}
		e.CaptureTime = t.In(models.CstZone).Format("15:04:05")
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Query failed", 500)
		log.Println("events rows failed:", err)
		return
	}
	if events == nil {
		events = []models.Event{}
	}
	json.NewEncoder(w).Encode(models.APIResponse{Count: len(events), Events: events})
}

func DevicesAPIHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query("SELECT d.id, d.last_seen, d.enabled, (SELECT filename FROM events e2 WHERE e2.device_id = d.id ORDER BY capture_time DESC LIMIT 1) as last_image FROM devices d ORDER BY d.last_seen DESC")
	if err != nil {
		http.Error(w, "Query failed", 500)
		log.Println("devices query failed:", err)
		return
	}
	defer rows.Close()
	var devices []models.DeviceInfo
	for rows.Next() {
		var d models.DeviceInfo
		var t sql.NullTime
		var lastImage sql.NullString
		var enabled int
		if err := rows.Scan(&d.ID, &t, &enabled, &lastImage); err != nil {
			log.Println("device scan failed:", err)
			continue
		}
		if t.Valid {
			d.LastActive = t.Time.In(models.CstZone).Format("15:04:05")
		}
		if lastImage.Valid {
			d.LastImage = lastImage.String
		}
		d.Enabled = enabled == 1
		devices = append(devices, d)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Query failed", 500)
		log.Println("devices rows failed:", err)
		return
	}
	if devices == nil {
		devices = []models.DeviceInfo{}
	}
	json.NewEncoder(w).Encode(devices)
}

func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "405", 405)
		return
	}
	if !requireCSRF(w, r) {
		return
	}
	var c models.Config
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, "Bad Request", 400)
		return
	}
	if err := config.UpdateConfig(func(cfg *models.Config) {
		cfg.AIEndpoint = c.AIEndpoint
		if c.AIKey != "" && c.AIKey != config.DefaultKey {
			cfg.AIKey = c.AIKey
		}
		cfg.AIModel = c.AIModel
		if c.DingWebhook != "" {
			cfg.DingWebhook = c.DingWebhook
		}
		if c.DeviceKey != "" {
			cfg.DeviceKey = c.DeviceKey
		}
		if c.AlertKeywords != "" {
			cfg.AlertKeywords = c.AlertKeywords
		}
		if c.UploadRetentionDays > 0 {
			cfg.UploadRetentionDays = c.UploadRetentionDays
		}
	}); err != nil {
		http.Error(w, "Save config failed", 500)
		log.Println("config save failed:", err)
		return
	}
	fmt.Fprint(w, `{"status":"ok"}`)
}

func DeleteDeviceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "405", 405)
		return
	}
	if !requireCSRF(w, r) {
		return
	}
	devID := r.URL.Query().Get("device_id")
	if !isSafeDeviceID(devID) {
		http.Error(w, "Invalid device_id", 400)
		return
	}
	rows, err := database.DB.Query("SELECT filename FROM events WHERE device_id=?", devID)
	if err != nil {
		http.Error(w, "Query failed", 500)
		log.Println("device events query failed:", err)
		return
	}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			log.Println("device event scan failed:", err)
			continue
		}
		_ = storage.RemoveImage(f)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Query failed", 500)
		log.Println("device events rows failed:", err)
		_ = rows.Close()
		return
	}
	if err := rows.Close(); err != nil {
		http.Error(w, "Query failed", 500)
		log.Println("device events rows close failed:", err)
		return
	}
	if _, err := database.DB.Exec("DELETE FROM events WHERE device_id=?", devID); err != nil {
		http.Error(w, "Delete events failed", 500)
		log.Println("delete device events failed:", err)
		return
	}
	if _, err := database.DB.Exec("DELETE FROM devices WHERE id=?", devID); err != nil {
		http.Error(w, "Delete device failed", 500)
		log.Println("delete device failed:", err)
		return
	}
	fmt.Fprintf(w, "OK")
}

func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "405", 405)
		return
	}
	if !requireCSRF(w, r) {
		return
	}
	n := r.URL.Query().Get("filename")
	if !isSafeFilename(n) {
		http.Error(w, "Invalid filename", 400)
		return
	}
	_ = storage.RemoveImage(n)
	if _, err := database.DB.Exec("DELETE FROM events WHERE filename=?", n); err != nil {
		http.Error(w, "Delete failed", 500)
		log.Println("delete event failed:", err)
		return
	}
	fmt.Fprintf(w, "OK")
}

func AvatarUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "405", 405)
		return
	}
	if !requireCSRF(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		http.Error(w, "Payload too large", 413)
		return
	}
	f, _, err := r.FormFile("avatar")
	if err != nil {
		http.Error(w, "Missing avatar", 400)
		return
	}
	defer f.Close()
	data, err := ioutil.ReadAll(io.LimitReader(f, 5<<20))
	if err != nil {
		http.Error(w, "Read avatar failed", 400)
		return
	}
	ext := supportedImageExt(data)
	if ext == "" {
		http.Error(w, "Unsupported avatar", 415)
		return
	}
	if err := os.MkdirAll("./uploads/avatars", 0755); err != nil {
		http.Error(w, "Create avatar dir failed", 500)
		log.Println("avatar dir failed:", err)
		return
	}
	n := fmt.Sprintf("avatar_%d_%s%s", time.Now().UnixNano(), security.NewToken(4), ext)
	d, err := os.Create("./uploads/avatars/" + n)
	if err != nil {
		http.Error(w, "Create avatar failed", 500)
		log.Println("avatar create failed:", err)
		return
	}
	defer d.Close()
	if _, err := d.Write(data); err != nil {
		http.Error(w, "Write avatar failed", 500)
		log.Println("avatar write failed:", err)
		return
	}
	var oldAvatar string
	if err := config.UpdateConfig(func(cfg *models.Config) {
		oldAvatar = cfg.Avatar
		cfg.Avatar = n
	}); err != nil {
		http.Error(w, "Save config failed", 500)
		log.Println("config save failed:", err)
		return
	}
	if oldAvatar != "" && oldAvatar != n && isSafeFilename(oldAvatar) {
		_ = os.Remove("./uploads/avatars/" + oldAvatar)
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "url": "/uploads/avatars/" + n})
}

func UpdateProfileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "405", 405)
		return
	}
	if !requireCSRF(w, r) {
		return
	}
	var d struct {
		Username string
		Password string
	}
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		http.Error(w, "Bad Request", 400)
		return
	}
	if err := config.UpdateConfig(func(cfg *models.Config) {
		if d.Username != "" {
			cfg.AdminUser = d.Username
		}
		if d.Password != "" {
			cfg.AdminPass = security.HashPassword(d.Password)
		}
	}); err != nil {
		http.Error(w, "Save config failed", 500)
		log.Println("profile save failed:", err)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

//AI与报警

func AnalyzeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != "POST" {
		http.Error(w, "405", 405)
		return
	}
	if !requireCSRF(w, r) {
		return
	}
	var payload struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Bad Request", 400)
		return
	}
	fname := payload.Filename
	if fname == "" {
		fname = r.URL.Query().Get("filename")
	}
	if !isSafeFilename(fname) {
		http.Error(w, "Invalid filename", 400)
		return
	}

	// 查一下这个文件属于哪个设备
	var deviceID string
	err := database.DB.QueryRow("SELECT device_id FROM events WHERE filename = ?", fname).Scan(&deviceID)
	if err != nil {
		http.Error(w, "Event not found", 404)
		return
	}

	if err := recordAnalysisQueued(fname); err != nil {
		http.Error(w, "Queue failed", 500)
		log.Println("analysis queue failed:", err)
		return
	}
	wakeAnalysisWorkers()
	json.NewEncoder(w).Encode(models.AnalysisResponse{Response: "已加入分析队列"})
}

func EventsUpdateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "405", 405)
		return
	}
	if !requireCSRF(w, r) {
		return
	}
	var payload struct {
		Filename string `json:"filename"`
		Status   string `json:"status"`
		Tags     string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Bad Request", 400)
		return
	}
	if !isSafeFilename(payload.Filename) {
		http.Error(w, "Invalid filename", 400)
		return
	}
	if _, err := database.DB.Exec(
		"UPDATE events SET status=COALESCE(NULLIF(?, ''), status), tags=COALESCE(NULLIF(?, ''), tags) WHERE filename=?",
		payload.Status,
		payload.Tags,
		payload.Filename,
	); err != nil {
		http.Error(w, "Update failed", 500)
		log.Println("event update failed:", err)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func QueueStatusHandler(w http.ResponseWriter, r *http.Request) {
	jobStats.Lock()
	processed := jobStats.Processed
	failed := jobStats.Failed
	active := jobStats.Active
	jobStats.Unlock()
	json.NewEncoder(w).Encode(map[string]int{
		"length":    countPendingAnalyses(),
		"capacity":  0,
		"processed": processed,
		"failed":    failed,
		"active":    active,
	})
}

func AIStatusHandler(w http.ResponseWriter, r *http.Request) {
	config.ConfigMu.RLock()
	endpoint := config.AppConfig.AIEndpoint
	model := config.AppConfig.AIModel
	keyConfigured := config.AppConfig.AIKey != "" && config.AppConfig.AIKey != config.DefaultKey && config.AppConfig.AIKey != "replace-me"
	config.ConfigMu.RUnlock()

	resp := map[string]interface{}{
		"endpoint_configured": endpoint != "",
		"key_configured":      keyConfigured,
		"model_configured":    model != "",
		"endpoint":            endpoint,
		"model":               model,
		"model_warning":       aiModelWarning(model),
		"request_timeout_sec": int(analysisHTTPTimeout.Seconds()),
		"queue_length":        countPendingAnalyses(),
		"queue_capacity":      0,
	}

	var last struct {
		Filename string
		Status   string
		Error    string
		Attempts int
	}
	err := database.DB.QueryRow("SELECT filename, IFNULL(analysis_status,''), IFNULL(analysis_error,''), IFNULL(analysis_attempts,0) FROM events WHERE IFNULL(analysis_status,'') IN ('failed','processing','queued') ORDER BY id DESC LIMIT 1").Scan(&last.Filename, &last.Status, &last.Error, &last.Attempts)
	if err == nil {
		resp["last_event"] = last
	} else if err != sql.ErrNoRows {
		http.Error(w, "Query failed", 500)
		log.Println("ai status query failed:", err)
		return
	}
	json.NewEncoder(w).Encode(resp)
}

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	if database.DB == nil {
		status = "db_error"
	} else if err := database.DB.Ping(); err != nil {
		status = "db_error"
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  status,
		"uptime":  int(time.Since(serverStart).Seconds()),
		"queue":   countPendingAnalyses(),
		"version": "v11.4",
	})
}

func checkAndAlert(analysis string, filename string, deviceID string) {
	fmt.Printf("🧐 AI分析结果: %s\n", analysis)

	config.ConfigMu.RLock()
	keywordStr := config.AppConfig.AlertKeywords
	config.ConfigMu.RUnlock()
	var dangerKeywords []string
	if keywordStr != "" {
		for _, kw := range strings.Split(keywordStr, ",") {
			trimmed := strings.TrimSpace(kw)
			if trimmed != "" {
				dangerKeywords = append(dangerKeywords, trimmed)
			}
		}
	}
	if len(dangerKeywords) == 0 {
		dangerKeywords = []string{"火", "烟", "倒", "血", "刀", "棍", "入侵", "陌生人", "打架", "攀爬", "求救", "Fire", "Smoke", "Knife", "Blood"}
	}

	triggered := false
	for _, kw := range dangerKeywords {
		if strings.Contains(analysis, kw) {
			fmt.Printf("🚨 触发告警! 关键词: %s\n", kw) //打印触发信息
			go sendDingTalk(analysis, filename, deviceID)
			// --- 🔥 新增：向网页端广播 JSON 警报 ---
			// 构造一个 JSON 数据，让前端好解析
			alertData := map[string]string{
				"type":      "ALERT",
				"device_id": deviceID,
				"time":      time.Now().Format("15:04:05"),
				"content":   analysis,
				"image":     "/uploads/" + filename,
				"keyword":   kw,
			}
			jsonBytes, _ := json.Marshal(alertData)

			// 📣 全员广播！
			stream.AlertBroker.Broadcast(string(jsonBytes))
			// ----------------------------------------

			triggered = true
			break

		}
	}

	if !triggered {
		fmt.Println("✅ 画面安全，未触发推送") //打印未触发信息
	}
}

func sendDingTalk(content string, filename string, deviceID string) {
	config.ConfigMu.RLock()
	webhook := config.AppConfig.DingWebhook
	config.ConfigMu.RUnlock()
	if webhook == "" {
		return
	}
	msg := models.DingMsg{MsgType: "markdown"}
	msg.Markdown.Title = "鹰眼安全警报"
	msg.Markdown.Text = fmt.Sprintf("###鹰眼系统安全预警\n\n**📷 设备**: %s\n\n**时间**: %s\n\n**AI 分析**: <font color=#FF0000>%s</font>\n\n**📸 证据文件**: %s", deviceID, time.Now().In(models.CstZone).Format("15:04:05"), content, filename)
	payload, _ := json.Marshal(msg)
	http.Post(webhook, "application/json", bytes.NewBuffer(payload))
}

func requireDeviceKey(w http.ResponseWriter, r *http.Request) bool {
	config.ConfigMu.RLock()
	deviceKey := config.AppConfig.DeviceKey
	config.ConfigMu.RUnlock()
	if deviceKey == "" {
		http.Error(w, "Device key is not configured", 503)
		return false
	}
	provided := r.Header.Get("X-Device-Key")
	if provided == "" {
		provided = r.URL.Query().Get("device_key")
	}
	if provided != deviceKey {
		http.Error(w, "Unauthorized", 401)
		return false
	}
	return true
}

func getSession(r *http.Request) (sessionInfo, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return sessionInfo{}, false
	}
	sessionMu.RLock()
	s, ok := sessions[c.Value]
	sessionMu.RUnlock()
	if !ok || time.Now().After(s.Expires) {
		return sessionInfo{}, false
	}
	return s, true
}

func requireCSRF(w http.ResponseWriter, r *http.Request) bool {
	session, ok := getSession(r)
	if !ok {
		http.Error(w, "Unauthorized", 401)
		return false
	}
	if r.Header.Get("X-CSRF-Token") != session.CSRF {
		http.Error(w, "CSRF invalid", 403)
		return false
	}
	return true
}

func setSessionCookies(w http.ResponseWriter, r *http.Request, sessionToken string, csrfToken string) {
	secure := r.TLS != nil
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    csrfToken,
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func clearSessionCookies(w http.ResponseWriter, r *http.Request) {
	secure := r.TLS != nil
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   -1,
	})
}

func newToken(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func StartAnalysisWorkers(count int) {
	if count <= 0 {
		count = 1
	}
	for i := 1; i <= count; i++ {
		go startDBWorker(i)
	}
}

func startDBWorker(id int) {
	fmt.Printf("AI 分析启动 (Worker %d Started)\n", id)
	for {
		job, ok := claimNextAnalysisJob()
		if !ok {
			select {
			case <-analysisWake:
			case <-time.After(5 * time.Second):
			}
			continue
		}
		processClaimedAnalysisJob(id, job)
	}
}

func RequeuePendingAnalyses() {
	wakeAnalysisWorkers()
}

func StartAnalysisScheduler() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		wakeAnalysisWorkers()
	}
}

func claimNextAnalysisJob() (AnalysisJob, bool) {
	finalizeExhaustedStaleAnalyses()
	rows, err := database.DB.Query(`
		SELECT filename, IFNULL(device_id, 'CAM-01')
		FROM events
		WHERE
			(
				IFNULL(analysis_status, 'pending') IN ('queued', 'pending')
				OR (analysis_status = 'failed' AND IFNULL(analysis_attempts, 0) < ?)
				OR (analysis_status = 'processing' AND updated_at < ?)
			)
			AND IFNULL(analysis_attempts, 0) < ?
		ORDER BY id ASC
		LIMIT 10`,
		analysisMaxAttempts,
		time.Now().Add(-analysisLeaseTTL),
		analysisMaxAttempts,
	)
	if err != nil {
		log.Println("analysis claim query failed:", err)
		return AnalysisJob{}, false
	}
	defer rows.Close()
	for rows.Next() {
		var job AnalysisJob
		if err := rows.Scan(&job.Filename, &job.DeviceID); err != nil {
			continue
		}
		result, err := database.DB.Exec(`
			UPDATE events
			SET analysis_status='processing',
				analysis_error='',
				analysis_attempts=IFNULL(analysis_attempts,0)+1
			WHERE filename=?
				AND (
					IFNULL(analysis_status, 'pending') IN ('queued', 'pending')
					OR (analysis_status = 'failed' AND IFNULL(analysis_attempts, 0) < ?)
					OR (analysis_status = 'processing' AND updated_at < ?)
				)
				AND IFNULL(analysis_attempts, 0) < ?`,
			job.Filename,
			analysisMaxAttempts,
			time.Now().Add(-analysisLeaseTTL),
			analysisMaxAttempts,
		)
		if err != nil {
			log.Println("analysis claim update failed:", err)
			continue
		}
		affected, err := result.RowsAffected()
		if err == nil && affected == 1 {
			return job, true
		}
	}
	if err := rows.Err(); err != nil {
		log.Println("analysis claim rows failed:", err)
	}
	return AnalysisJob{}, false
}

func finalizeExhaustedStaleAnalyses() {
	_, err := database.DB.Exec(`
		UPDATE events
		SET analysis_status='failed',
			analysis_error='analysis timed out after max attempts'
		WHERE analysis_status='processing'
			AND IFNULL(analysis_attempts, 0) >= ?
			AND updated_at < ?`,
		analysisMaxAttempts,
		time.Now().Add(-analysisLeaseTTL),
	)
	if err != nil {
		log.Println("analysis stale finalize failed:", err)
	}
}

func processClaimedAnalysisJob(workerID int, job AnalysisJob) {
	jobStats.Lock()
	jobStats.Active++
	jobStats.Unlock()
	defer func() {
		jobStats.Lock()
		jobStats.Active--
		jobStats.Unlock()
		if recovered := recover(); recovered != nil {
			recordAnalysisFailure(job.Filename, fmt.Sprintf("analysis panic: %v", recovered))
		}
	}()
	fmt.Printf("Worker %d 正在处理任务: %s (设备: %s)\n", workerID, job.Filename, job.DeviceID)
	performAnalysis(job.Filename, job.DeviceID)
}

func wakeAnalysisWorkers() {
	select {
	case analysisWake <- struct{}{}:
	default:
	}
}

func countPendingAnalyses() int {
	var count int
	err := database.DB.QueryRow(`
		SELECT COUNT(*)
		FROM events
		WHERE IFNULL(analysis_status, 'pending') IN ('queued', 'pending')
			OR (analysis_status = 'processing' AND updated_at < ?)`,
		time.Now().Add(-analysisLeaseTTL),
	).Scan(&count)
	if err != nil {
		log.Println("analysis count failed:", err)
	}
	return count
}

func StartRetentionWorker() {
	cleanupOldUploads()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		cleanupOldUploads()
	}
}

//核心 AI 逻辑
func performAnalysis(filename string, deviceID string) {
	start := time.Now()
	//读取文件
	imgBytes, err := storage.ReadImage(filename)
	if err != nil {
		fmt.Println("文件读取失败:", err)
		recordAnalysisFailure(filename, err.Error())
		return
	}
	imageURL, imageInfo, err := prepareAIImageDataURL(imgBytes)
	if err != nil {
		recordAnalysisFailure(filename, err.Error())
		return
	}

	//准备配置
	config.ConfigMu.RLock()
	ep := config.AppConfig.AIEndpoint
	key := config.AppConfig.AIKey
	model := config.AppConfig.AIModel
	config.ConfigMu.RUnlock()
	if ep == "" {
		recordAnalysisFailure(filename, "AI endpoint is empty")
		return
	}
	if model == "" {
		recordAnalysisFailure(filename, "AI model is empty")
		return
	}
	if key == "" || key == config.DefaultKey || key == "replace-me" {
		recordAnalysisFailure(filename, "AI key is not configured")
		return
	}

	//构造请求
	type Msg struct {
		Role    string        `json:"role"`
		Content []interface{} `json:"content"`
	}
	type ImgURL struct {
		URL string `json:"url"`
	}
	reqBody := map[string]interface{}{
		"model":       model,
		"max_tokens":  160,
		"temperature": 0.2,
		"stream":      false,
		"messages": []Msg{
			{
				Role: "user",
				Content: []interface{}{
					map[string]string{"type": "text", "text": "请用中文简明判断这张安防画面是否有危险。只输出：危险等级(低/中/高)、关键目标、原因。"},
					map[string]interface{}{"type": "image_url", "image_url": ImgURL{URL: imageURL}},
				},
			},
		},
	}

	p, err := json.Marshal(reqBody)
	if err != nil {
		recordAnalysisFailure(filename, "AI request marshal failed: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), analysisHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", ep, bytes.NewBuffer(p))
	if err != nil {
		recordAnalysisFailure(filename, "AI request create failed: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	log.Printf("AI request started: file=%s device=%s endpoint=%s model=%s image=%s", filename, deviceID, ep, model, imageInfo)

	client := &http.Client{
		Timeout: analysisHTTPTimeout + 2*time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: analysisHTTPTimeout,
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       30 * time.Second,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("AI 请求失败:", err)
		recordAnalysisFailure(filename, "AI request failed after "+time.Since(start).Round(time.Millisecond).String()+": "+err.Error())
		return
	}
	defer resp.Body.Close()
	body, readErr := ioutil.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		recordAnalysisFailure(filename, "AI response read failed: "+readErr.Error())
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		recordAnalysisFailure(filename, fmt.Sprintf("AI status %d: %s", resp.StatusCode, truncateForLog(string(body), 300)))
		return
	}

	//解析结果
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil || len(apiResp.Choices) == 0 {
		fmt.Println("AI 无响应或解析失败")
		if err != nil {
			recordAnalysisFailure(filename, "AI parse failed: "+err.Error())
		} else {
			recordAnalysisFailure(filename, "AI parse failed: empty choices")
		}
		return
	}
	ans := apiResp.Choices[0].Message.Content
	if strings.TrimSpace(ans) == "" {
		recordAnalysisFailure(filename, "AI parse failed: empty content")
		return
	}

	// 后续处理
	log.Printf("AI analysis done: file=%s duration=%s result=%s", filename, time.Since(start).Round(time.Millisecond), ans)
	if _, err := database.DB.Exec("UPDATE events SET ai_analysis = ?, analysis_status='done', analysis_error='' WHERE filename = ?", ans, filename); err != nil {
		log.Println("analysis update failed:", err)
	}
	jobStats.Lock()
	jobStats.Processed++
	jobStats.Unlock()
	checkAndAlert(ans, filename, deviceID)
}

func recordAnalysisQueued(filename string) error {
	_, err := database.DB.Exec("UPDATE events SET analysis_status='queued', analysis_error='', analysis_attempts=0 WHERE filename=?", filename)
	return err
}

func recordAnalysisFailure(filename string, msg string) {
	_, _ = database.DB.Exec("UPDATE events SET analysis_status='failed', analysis_error=? WHERE filename=?", msg, filename)
	jobStats.Lock()
	jobStats.Failed++
	jobStats.Unlock()
}

func prepareAIImageDataURL(data []byte) (string, string, error) {
	if len(data) < 512 {
		return "", "", fmt.Errorf("image too small")
	}
	originalMime := http.DetectContentType(data[:512])
	optimized, info, err := optimizeImageForAI(data, originalMime)
	if err == nil {
		return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(optimized), info, nil
	}
	switch originalMime {
	case "image/jpeg", "image/png", "image/webp":
		return "data:" + originalMime + ";base64," + base64.StdEncoding.EncodeToString(data), fmt.Sprintf("%s original=%dB optimize_skipped=%v", originalMime, len(data), err), nil
	default:
		return "", "", fmt.Errorf("unsupported AI image mime: %s", originalMime)
	}
}

func optimizeImageForAI(data []byte, mime string) ([]byte, string, error) {
	if mime != "image/jpeg" && mime != "image/png" {
		return nil, "", fmt.Errorf("unsupported optimizer mime %s", mime)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	b := img.Bounds()
	srcW := b.Dx()
	srcH := b.Dy()
	maxEdge := 960
	dstW, dstH := scaledDimensions(srcW, srcH, maxEdge)
	var out image.Image = img
	if dstW != srcW || dstH != srcH {
		out = resizeNearest(img, dstW, dstH)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 72}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), fmt.Sprintf("%s %dx%d->%dx%d %dB->%dB", mime, srcW, srcH, dstW, dstH, len(data), buf.Len()), nil
}

func scaledDimensions(width, height, maxEdge int) (int, int) {
	if width <= 0 || height <= 0 || (width <= maxEdge && height <= maxEdge) {
		return width, height
	}
	if width >= height {
		return maxEdge, maxInt(1, height*maxEdge/width)
	}
	return maxInt(1, width*maxEdge/height), maxEdge
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func resizeNearest(src image.Image, dstW, dstH int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()
	for y := 0; y < dstH; y++ {
		sy := srcBounds.Min.Y + y*srcH/dstH
		for x := 0; x < dstW; x++ {
			sx := srcBounds.Min.X + x*srcW/dstW
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func cleanupOldUploads() {
	config.ConfigMu.RLock()
	days := config.AppConfig.UploadRetentionDays
	config.ConfigMu.RUnlock()
	if days <= 0 {
		return
	}
	cutoff := time.Now().In(models.CstZone).AddDate(0, 0, -days)
	rows, err := database.DB.Query("SELECT filename FROM events WHERE capture_time < ?", cutoff)
	if err != nil {
		log.Println("retention query failed:", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err == nil && isSafeFilename(filename) {
			_ = storage.RemoveImage(filename)
		}
	}
	if _, err := database.DB.Exec("DELETE FROM events WHERE capture_time < ?", cutoff); err != nil {
		log.Println("retention delete failed:", err)
	}
}
