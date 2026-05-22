package web

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"hawkeye/internal/models"
)

func TestAppTemplateUsesConfiguredProfile(t *testing.T) {
	tmpl, err := template.ParseFS(Content, "templates/app.html")
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = tmpl.Execute(&out, models.Config{
		AdminUser:           "alice",
		Avatar:              "avatar_test.jpg",
		AIEndpoint:          "http://example.test",
		AIKey:               "key",
		AIModel:             "model",
		DeviceKey:           "device-key",
		AlertKeywords:       "fire",
		UploadRetentionDays: 7,
	})
	if err != nil {
		t.Fatal(err)
	}

	html := out.String()
	if !strings.Contains(html, `/uploads/avatars/avatar_test.jpg`) {
		t.Fatal("app template did not render configured avatar")
	}
	if !strings.Contains(html, `>alice</h2>`) {
		t.Fatal("app template did not render configured username")
	}
}
