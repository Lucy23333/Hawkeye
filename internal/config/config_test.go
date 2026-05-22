package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hawkeye/internal/models"
	"hawkeye/internal/security"
)

func TestInitConfigCreatesSecureConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hawkeye", "config.json")
	t.Setenv("HAWKEYE_CONFIG", path)
	t.Setenv("ADMIN_PASS", "test-password")
	t.Setenv("DEVICE_KEY", "device-secret")

	if err := InitConfig(); err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}
	if AppConfig.AdminUser != "admin" {
		t.Fatalf("AdminUser=%q", AppConfig.AdminUser)
	}
	if !security.IsHashedPassword(AppConfig.AdminPass) {
		t.Fatal("AdminPass was not hashed")
	}
	if AppConfig.DeviceKey != "device-secret" {
		t.Fatalf("DeviceKey=%q", AppConfig.DeviceKey)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("config mode=%v want 0600", got)
	}
}

func TestInitConfigRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{bad json"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HAWKEYE_CONFIG", path)

	if err := InitConfig(); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestUpdateConfigPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("HAWKEYE_CONFIG", path)
	if err := InitConfig(); err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}

	if err := UpdateConfig(func(cfg *models.Config) {
		cfg.AIModel = "test-model"
	}); err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "test-model") {
		t.Fatalf("config file did not contain update: %s", string(data))
	}
}
