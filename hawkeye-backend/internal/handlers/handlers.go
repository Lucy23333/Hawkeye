package handlers

import (
	"bytes"
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
	"hawkeye/internal/stream"
	"html/template"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type AnalysisJob struct {
	Filename string
	DeviceID string
}
// 定义一个带缓冲的通道，充当“任务队列”
// 缓冲区设为 100，意味着即使 AI 挂了，也能先暂存 100 张图
var JobQueue = make(chan AnalysisJob, 100)

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
	t.Execute(w, data)
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
	json.NewDecoder(r.Body).Decode(&c)
	config.ConfigMu.RLock()
	u := config.AppConfig.AdminUser
	p := config.AppConfig.AdminPass
	config.ConfigMu.RUnlock()
	if c.Username == u && c.Password == p {
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
	r.ParseMultipartForm(10 << 20)
	file, _, err := r.FormFile("image")
	if err != nil {
		return
	}
	defer file.Close()
	imgData, _ := ioutil.ReadAll(file)
	deviceID := r.FormValue("device_id")
	if deviceID == "" {
		deviceID = "CAM-01"
	}
	mode := r.FormValue("mode")

	var enabled int
	if err := database.DB.QueryRow("SELECT enabled FROM devices WHERE id=?", deviceID).Scan(&enabled); err == nil && enabled == 0 {
		http.Error(w, "Device disabled", 403)
		return
	}

	now := time.Now().In(models.CstZone)
	database.DB.Exec("INSERT INTO devices (id, last_seen, enabled) VALUES (?,?,1) ON DUPLICATE KEY UPDATE last_seen=VALUES(last_seen)", deviceID, now)

	// 1. 广播流
	stream.BroadcastFrame(deviceID, imgData)

	// 2. 纯流模式，不保存
	if mode == "stream" {
		return
	}

	// 3. 抓拍模式：保存到硬盘和数据库
	os.MkdirAll("./uploads", 0755)
	n := fmt.Sprintf("%s_%s", now.Format("20060102-150405"), "evidence.jpg")
	ioutil.WriteFile("./uploads/"+n, imgData, 0644)
	database.DB.Exec("INSERT INTO events (filename, capture_time, device_id) VALUES (?,?,?)", n, now, deviceID)

	// 🔥 新增：自动触发 AI 分析
	// 只有非流模式(stream)才分析，避免浪费 Token
	if mode != "stream" {
		select {
		case JobQueue <- AnalysisJob{Filename: n, DeviceID: deviceID}:
			fmt.Println("✅ 自动推入分析队列:", n)
		default:
			fmt.Println("⚠️ 队列已满，跳过自动分析:", n)
		}
	}

	fmt.Fprintf(w, "OK")
}

func EventsAPIHandler(w http.ResponseWriter, r *http.Request) {
	rows, _ := database.DB.Query("SELECT id, filename, capture_time, IFNULL(ai_analysis, ''), IFNULL(device_id, 'CAM-01'), IFNULL(status, 'open'), IFNULL(tags, '') FROM events ORDER BY id DESC LIMIT 50")
	defer rows.Close()
	var events []models.Event
	for rows.Next() {
		var e models.Event
		var t time.Time
		rows.Scan(&e.ID, &e.Filename, &t, &e.AIAnalysis, &e.DeviceID, &e.Status, &e.Tags)
		e.CaptureTime = t.In(models.CstZone).Format("15:04:05")
		events = append(events, e)
	}
	if events == nil {
		events = []models.Event{}
	}
	json.NewEncoder(w).Encode(models.APIResponse{Count: len(events), Events: events})
}

func DevicesAPIHandler(w http.ResponseWriter, r *http.Request) {
	rows, _ := database.DB.Query("SELECT d.id, d.last_seen, d.enabled, (SELECT filename FROM events e2 WHERE e2.device_id = d.id ORDER BY capture_time DESC LIMIT 1) as last_image FROM devices d ORDER BY d.last_seen DESC")
	defer rows.Close()
	var devices []models.DeviceInfo
	for rows.Next() {
		var d models.DeviceInfo
		var t sql.NullTime
		var lastImage sql.NullString
		var enabled int
		rows.Scan(&d.ID, &t, &enabled, &lastImage)
		if t.Valid {
			d.LastActive = t.Time.In(models.CstZone).Format("15:04:05")
		}
		if lastImage.Valid {
			d.LastImage = lastImage.String
		}
		d.Enabled = enabled == 1
		devices = append(devices, d)
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
	json.NewDecoder(r.Body).Decode(&c)
	config.UpdateConfig(func(cfg *models.Config) {
		cfg.AIEndpoint = c.AIEndpoint
		cfg.AIKey = c.AIKey
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
	})
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
	rows, _ := database.DB.Query("SELECT filename FROM events WHERE device_id=?", devID)
	for rows.Next() {
		var f string
		rows.Scan(&f)
		os.Remove("./uploads/" + f)
	}
	rows.Close()
	database.DB.Exec("DELETE FROM events WHERE device_id=?", devID)
	database.DB.Exec("DELETE FROM devices WHERE id=?", devID)
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
	os.Remove("./uploads/" + n)
	database.DB.Exec("DELETE FROM events WHERE filename=?", n)
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
	r.ParseMultipartForm(10 << 20)
	f, h, e := r.FormFile("avatar")
	if e != nil {
		return
	}
	defer f.Close()
	os.MkdirAll("./uploads/avatars", 0755)
	n := fmt.Sprintf("avatar_%d%s", time.Now().Unix(), filepath.Ext(h.Filename))
	d, _ := os.Create("./uploads/avatars/" + n)
	defer d.Close()
	io.Copy(d, f)
	config.UpdateConfig(func(cfg *models.Config) {
		cfg.Avatar = n
	})
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
	json.NewDecoder(r.Body).Decode(&d)
	config.UpdateConfig(func(cfg *models.Config) {
		if d.Username != "" {
			cfg.AdminUser = d.Username
		}
		if d.Password != "" {
			cfg.AdminPass = d.Password
		}
	})
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
	_ = json.NewDecoder(r.Body).Decode(&payload)
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
		deviceID = "UNKNOWN"
	}

	// 🔥 核心修改：把任务扔进队列，而不是自己跑
	select {
	case JobQueue <- AnalysisJob{Filename: fname, DeviceID: deviceID}:
		// 成功入队
		json.NewEncoder(w).Encode(models.AnalysisResponse{Response: "⏳ 已加入分析队列 (Queued)"})
	default:
		// 队列满了 (比如积压了100个)
		json.NewEncoder(w).Encode(models.AnalysisResponse{Error: "🔥 系统繁忙，队列已满"})
	}
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
	_, _ = database.DB.Exec(
		"UPDATE events SET status=COALESCE(NULLIF(?, ''), status), tags=COALESCE(NULLIF(?, ''), tags) WHERE filename=?",
		payload.Status,
		payload.Tags,
		payload.Filename,
	)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func QueueStatusHandler(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]int{
		"length":   len(JobQueue),
		"capacity": cap(JobQueue),
	})
}

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	if err := database.DB.Ping(); err != nil {
		status = "db_error"
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  status,
		"uptime":  int(time.Since(serverStart).Seconds()),
		"queue":   len(JobQueue),
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
		return true
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

func getClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, items: map[string]*rateWindow{}}
}

func (rl *rateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	win, ok := rl.items[key]
	if !ok || time.Since(win.start) > rl.window {
		rl.items[key] = &rateWindow{start: time.Now(), count: 1}
		return true
	}
	if win.count >= rl.limit {
		return false
	}
	win.count++
	return true
}

func isSafeFilename(name string) bool {
	if name == "" {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	return filepath.Base(name) == name
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


func StartWorker() {
	fmt.Println("AI 分析启动 (Worker Started)")
	for job := range JobQueue {
		fmt.Printf("正在处理任务: %s (设备: %s)\n", job.Filename, job.DeviceID)
		performAnalysis(job.Filename, job.DeviceID)
	}
}

//核心 AI 逻辑 
func performAnalysis(filename string, deviceID string) {
	//读取文件
	imgBytes, err := ioutil.ReadFile(filepath.Join("./uploads", filename))
	if err != nil {
		fmt.Println("文件读取失败:", err)
		return
	}
	b64 := base64.StdEncoding.EncodeToString(imgBytes)

	//准备配置
	config.ConfigMu.RLock()
	ep := config.AppConfig.AIEndpoint
	key := config.AppConfig.AIKey
	model := config.AppConfig.AIModel
	config.ConfigMu.RUnlock()

	//构造请求
	type Msg struct {
		Role    string      `json:"role"`
		Content []interface{} `json:"content"`
	}
	type ImgURL struct {
		URL string `json:"url"`
	}
	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 300,
		"stream":     false,
		"messages": []Msg{
			{
				Role: "user",
				Content: []interface{}{
					map[string]string{"type": "text", "text": "Describe the danger level and details in this image."},
					map[string]interface{}{"type": "image_url", "image_url": ImgURL{URL: "data:image/jpeg;base64," + b64}},
				},
			},
		},
	}

	p, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", ep, bytes.NewBuffer(p))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("AI 请求失败:", err)
		return
	}
	defer resp.Body.Close()

	//解析结果
	body, _ := ioutil.ReadAll(resp.Body)
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil || len(apiResp.Choices) == 0 {
		fmt.Println("AI 无响应或解析失败")
		return
	}
	ans := apiResp.Choices[0].Message.Content

	// 后续处理
	fmt.Printf(" AI 分析完成: %s\n", ans)
	database.DB.Exec("UPDATE events SET ai_analysis = ? WHERE filename = ?", ans, filename)
	checkAndAlert(ans, filename, deviceID)
}