package config

import (
	"encoding/json"
	"hawkeye/internal/models"
	"io/ioutil"
	"os"
	"sync"
)

const (
	ConfigFile = "config.json"
	DefaultKey   = "YOUR_API_KEY_HERE"
	DefaultModel = "Qwen/Qwen2-VL-72B-Instruct"
)

var (
	AppConfig models.Config
	ConfigMu  sync.RWMutex
)

func InitConfig() {
	defaultConfig := models.Config{
		AIEndpoint: getEnvOrDefault("AI_ENDPOINT", "https://api.siliconflow.cn/v1/chat/completions"),
		AIKey:      getEnvOrDefault("AI_KEY", DefaultKey),
		AIModel:    getEnvOrDefault("AI_MODEL", DefaultModel),
		AdminUser:  getEnvOrDefault("ADMIN_USER", "admin"),
		AdminPass:  getEnvOrDefault("ADMIN_PASS", "admin"),
		Avatar:     "",
		DingWebhook: getEnvOrDefault("DING_WEBHOOK", ""),
		DeviceKey:  getEnvOrDefault("DEVICE_KEY", ""),
		AlertKeywords: getEnvOrDefault("ALERT_KEYWORDS", "火,烟,倒,血,刀,棍,入侵,陌生人,打架,攀爬,求救,Fire,Smoke,Knife,Blood"),
	}
	file, err := ioutil.ReadFile(ConfigFile)
	if err != nil {
		AppConfig = defaultConfig
		SaveConfig()
	} else {
		json.Unmarshal(file, &AppConfig)
		if AppConfig.AdminUser == "" {
			AppConfig.AdminUser = "admin"
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