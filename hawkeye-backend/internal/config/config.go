package config

import (
	"encoding/json"
	"hawkeye/internal/models"
	"hawkeye/internal/security"
	"io/ioutil"
	"os"
	"strconv"
	"sync"
)

const (
	ConfigFile   = "config.json"
	DefaultKey   = "YOUR_API_KEY_HERE"
	DefaultModel = "Qwen/Qwen2-VL-72B-Instruct"
)

var (
	AppConfig models.Config
	ConfigMu  sync.RWMutex
)

func InitConfig() {
	deviceKey := getEnvOrDefault("DEVICE_KEY", "")
	if deviceKey == "" {
		deviceKey = security.NewToken(24)
	}
	defaultConfig := models.Config{
		AIEndpoint:          getEnvOrDefault("AI_ENDPOINT", "https://api.siliconflow.cn/v1/chat/completions"),
		AIKey:               getEnvOrDefault("AI_KEY", DefaultKey),
		AIModel:             getEnvOrDefault("AI_MODEL", DefaultModel),
		AdminUser:           getEnvOrDefault("ADMIN_USER", "admin"),
		AdminPass:           getEnvOrDefault("ADMIN_PASS", "admin"),
		Avatar:              "",
		DingWebhook:         getEnvOrDefault("DING_WEBHOOK", ""),
		DeviceKey:           deviceKey,
		AlertKeywords:       getEnvOrDefault("ALERT_KEYWORDS", "火,烟,倒,血,刀,棍,入侵,陌生人,打架,攀爬,求救,Fire,Smoke,Knife,Blood"),
		UploadRetentionDays: getEnvIntOrDefault("UPLOAD_RETENTION_DAYS", 30),
	}
	if !security.IsHashedPassword(defaultConfig.AdminPass) {
		defaultConfig.AdminPass = security.HashPassword(defaultConfig.AdminPass)
	}
	file, err := ioutil.ReadFile(ConfigFile)
	if err != nil {
		AppConfig = defaultConfig
		SaveConfig()
	} else {
		json.Unmarshal(file, &AppConfig)
		changed := false
		if AppConfig.AdminUser == "" {
			AppConfig.AdminUser = "admin"
		}
		if AppConfig.AdminPass == "" {
			AppConfig.AdminPass = defaultConfig.AdminPass
			changed = true
		}
		if AppConfig.DeviceKey == "" {
			AppConfig.DeviceKey = deviceKey
			changed = true
		}
		if AppConfig.AlertKeywords == "" {
			AppConfig.AlertKeywords = defaultConfig.AlertKeywords
			changed = true
		}
		if AppConfig.UploadRetentionDays == 0 {
			AppConfig.UploadRetentionDays = defaultConfig.UploadRetentionDays
			changed = true
		}
		if envKey := os.Getenv("AI_KEY"); envKey != "" {
			AppConfig.AIKey = envKey
		}
		if envPass := os.Getenv("ADMIN_PASS"); envPass != "" {
			AppConfig.AdminPass = envPass
			changed = true
		}
		if !security.IsHashedPassword(AppConfig.AdminPass) {
			AppConfig.AdminPass = security.HashPassword(AppConfig.AdminPass)
			changed = true
		}
		if changed {
			SaveConfig()
		}
	}
}

func SaveConfig() error {
	ConfigMu.Lock()
	defer ConfigMu.Unlock()
	return saveConfigUnlocked()
}

func UpdateConfig(update func(*models.Config)) error {
	ConfigMu.Lock()
	defer ConfigMu.Unlock()
	update(&AppConfig)
	return saveConfigUnlocked()
}

func saveConfigUnlocked() error {
	data, _ := json.MarshalIndent(AppConfig, "", "  ")
	return ioutil.WriteFile(ConfigFile, data, 0644)
}

func getEnvOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvIntOrDefault(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		parsed, err := strconv.Atoi(val)
		if err == nil {
			return parsed
		}
	}
	return fallback
}
