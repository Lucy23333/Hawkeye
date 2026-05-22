package handlers

import (
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

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

func isSafeDeviceID(id string) bool {
	if id == "" || len(id) > 50 {
		return false
	}
	for _, r := range id {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == ':' {
			continue
		}
		return false
	}
	return true
}

func isSupportedImage(data []byte) bool {
	return supportedImageExt(data) != ""
}

func supportedImageExt(data []byte) string {
	if len(data) < 512 {
		return ""
	}
	mime := http.DetectContentType(data[:512])
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

func truncateForLog(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func aiModelWarning(model string) string {
	lower := strings.ToLower(model)
	if strings.Contains(lower, "vl") || strings.Contains(lower, "vision") {
		return ""
	}
	if model == "" {
		return "AI model is empty"
	}
	return "model name does not look like a vision/VL model"
}
