package config

import (
	"encoding/json"
	"hawkeye/internal/models"
	"io/ioutil"
	"sync"
)

const (
	ConfigFile = "config.json"
	DefaultKey   = "YOUR_API_KEY_HERE" 
	DefaultModel = "Qwen/Qwen2-VL-72B-Instruct"
	HardcodedWebhook = "https://oapi.dingtalk.com/robot/send?access_token=935936d8d856569d8e2b8a659dee224c0dbf4e8538bf0580aa8ad55753150f77"
)

var (
	AppConfig models.Config
	ConfigMu  sync.RWMutex
)

func InitConfig() {
	defaultConfig := models.Config{
		AIEndpoint: "https://api.siliconflow.cn/v1/chat/completions",
		AIKey:      DefaultKey,
		AIModel:    DefaultModel,
		AdminUser:  "admin",
		AdminPass:  "admin",
		Avatar:     "",
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
	data, _ := json.MarshalIndent(AppConfig, "", "  ")
	return ioutil.WriteFile(ConfigFile, data, 0644)
}