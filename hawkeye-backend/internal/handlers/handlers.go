package handlers

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hawkeye/internal/config"
	"hawkeye/internal/database"
	"hawkeye/internal/models"
	"hawkeye/internal/stream"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
		http.SetCookie(w, &http.Cookie{Name: "token", Value: "ok", Path: "/"})
		fmt.Fprint(w, "OK")
	} else {
		http.Error(w, "Fail", 401)
	}
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "token", Value: "", MaxAge: -1, Path: "/"})
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
		c, e := r.Cookie("token")
		if e != nil || c.Value != "ok" {
			http.Redirect(w, r, "/login", 302)
			return
		}
		n(w, r)
	}
}

//API Handlers

func StreamHandler(w http.ResponseWriter, r *http.Request) {
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

func UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		return
	}
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

	// 1. 广播流
	stream.BroadcastFrame(deviceID, imgData)

	// 2. 纯流模式，不保存
	if mode == "stream" {
		return
	}

	// 3. 抓拍模式：保存到硬盘和数据库
	os.MkdirAll("./uploads", 0755)
	now := time.Now().In(models.CstZone)
	n := fmt.Sprintf("%s_%s", now.Format("20060102-150405"), "evidence.jpg")
	ioutil.WriteFile("./uploads/"+n, imgData, 0644)
	database.DB.Exec("INSERT INTO events (filename, capture_time, device_id) VALUES (?,?,?)", n, now, deviceID)
	fmt.Fprintf(w, "OK")
}

func EventsAPIHandler(w http.ResponseWriter, r *http.Request) {
	rows, _ := database.DB.Query("SELECT id, filename, capture_time, IFNULL(ai_analysis, ''), IFNULL(device_id, 'CAM-01') FROM events ORDER BY id DESC LIMIT 50")
	defer rows.Close()
	var events []models.Event
	for rows.Next() {
		var e models.Event
		var t time.Time
		rows.Scan(&e.ID, &e.Filename, &t, &e.AIAnalysis, &e.DeviceID)
		e.CaptureTime = t.In(models.CstZone).Format("15:04:05")
		events = append(events, e)
	}
	if events == nil {
		events = []models.Event{}
	}
	json.NewEncoder(w).Encode(models.APIResponse{Count: len(events), Events: events})
}

func DevicesAPIHandler(w http.ResponseWriter, r *http.Request) {
	rows, _ := database.DB.Query("SELECT device_id, MAX(capture_time) as last_active, (SELECT filename FROM events e2 WHERE e2.device_id = e1.device_id ORDER BY capture_time DESC LIMIT 1) as last_image FROM events e1 GROUP BY device_id")
	defer rows.Close()
	var devices []models.DeviceInfo
	for rows.Next() {
		var d models.DeviceInfo
		var t time.Time
		rows.Scan(&d.ID, &t, &d.LastImage)
		d.LastActive = t.In(models.CstZone).Format("15:04:05")
		devices = append(devices, d)
	}
	if devices == nil {
		devices = []models.DeviceInfo{}
	}
	json.NewEncoder(w).Encode(devices)
}

func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	var c models.Config
	json.NewDecoder(r.Body).Decode(&c)
	config.ConfigMu.Lock()
	config.AppConfig.AIEndpoint = c.AIEndpoint
	config.AppConfig.AIKey = c.AIKey
	config.AppConfig.AIModel = c.AIModel
	config.SaveConfig()
	config.ConfigMu.Unlock()
	fmt.Fprint(w, `{"status":"ok"}`)
}

func DeleteDeviceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "405", 405)
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
	fmt.Fprintf(w, "OK")
}

func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	n := r.URL.Query().Get("filename")
	os.Remove("./uploads/" + n)
	database.DB.Exec("DELETE FROM events WHERE filename=?", n)
	fmt.Fprintf(w, "OK")
}

func AvatarUploadHandler(w http.ResponseWriter, r *http.Request) {
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
	config.ConfigMu.Lock()
	config.AppConfig.Avatar = n
	config.SaveConfig()
	config.ConfigMu.Unlock()
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "url": "/uploads/avatars/" + n})
}

func UpdateProfileHandler(w http.ResponseWriter, r *http.Request) {
	var d struct {
		Username string
		Password string
	}
	json.NewDecoder(r.Body).Decode(&d)
	config.ConfigMu.Lock()
	if d.Username != "" {
		config.AppConfig.AdminUser = d.Username
	}
	if d.Password != "" {
		config.AppConfig.AdminPass = d.Password
	}
	config.SaveConfig()
	config.ConfigMu.Unlock()
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

//AI与报警

func AnalyzeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fname := r.URL.Query().Get("filename")
	var deviceID string
	database.DB.QueryRow("SELECT device_id FROM events WHERE filename = ?", fname).Scan(&deviceID)
	if deviceID == "" {
		deviceID = "UNKNOWN"
	}
	imgBytes, err := ioutil.ReadFile(filepath.Join("./uploads", fname))
	if err != nil {
		json.NewEncoder(w).Encode(models.AnalysisResponse{Error: "File not found"})
		return
	}
	b64 := base64.StdEncoding.EncodeToString(imgBytes)
	
	config.ConfigMu.RLock()
	ep := config.AppConfig.AIEndpoint
	key := config.AppConfig.AIKey
	model := config.AppConfig.AIModel
	config.ConfigMu.RUnlock()

	//构造SiliconFlow/OpenAI格式请求
	type Msg struct {
		Role    string      `json:"role"`
		Content []interface{} `json:"content"` // 使用 interface{} 混合 Text 和 ImageURL
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
		json.NewEncoder(w).Encode(models.AnalysisResponse{Error: "Net Error"})
		return
	}
	defer resp.Body.Close()
	
	//解析响应
	body, _ := ioutil.ReadAll(resp.Body)
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	
	if err := json.Unmarshal(body, &apiResp); err != nil || len(apiResp.Choices) == 0 {
		json.NewEncoder(w).Encode(models.AnalysisResponse{Error: "AI No Reply"})
		return
	}
	ans := apiResp.Choices[0].Message.Content
	
	//异步处理报警
	go checkAndAlert(ans, fname, deviceID)
	//更新数据库
	go database.DB.Exec("UPDATE events SET ai_analysis = ? WHERE filename = ?", ans, fname)
	
	json.NewEncoder(w).Encode(models.AnalysisResponse{Response: ans})
}

func checkAndAlert(analysis string, filename string, deviceID string) {
    fmt.Printf("🧐 AI分析结果: %s\n", analysis)

    dangerKeywords := []string{"火", "烟", "倒", "血", "刀", "棍", "入侵", "陌生人", "打架", "攀爬", "求救", "Fire", "Smoke", "Knife", "Blood"}
    
    triggered := false
    for _, kw := range dangerKeywords {
        if strings.Contains(analysis, kw) {
            fmt.Printf("🚨 触发告警! 关键词: %s\n", kw) //打印触发信息
            go sendDingTalk(analysis, filename, deviceID)
            triggered = true
            break
        }
    }
    
    if !triggered {
        fmt.Println("✅ 画面安全，未触发推送") //打印未触发信息
    }
}

func sendDingTalk(content string, filename string, deviceID string) {
	msg := models.DingMsg{MsgType: "markdown"}
	msg.Markdown.Title = "🚨 鹰眼安全警报"
	msg.Markdown.Text = fmt.Sprintf("### 🦅 鹰眼系统安全预警\n\n**📷 设备**: %s\n\n**⏰ 时间**: %s\n\n**🤖 AI 分析**: <font color=#FF0000>%s</font>\n\n**📸 证据文件**: %s", deviceID, time.Now().In(models.CstZone).Format("15:04:05"), content, filename)
	payload, _ := json.Marshal(msg)
	http.Post(config.HardcodedWebhook, "application/json", bytes.NewBuffer(payload))
}