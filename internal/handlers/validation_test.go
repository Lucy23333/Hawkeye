package handlers

import (
	"net/http"
	"testing"
	"time"
)

func TestSafeFilename(t *testing.T) {
	tests := map[string]bool{
		"image.jpg":         true,
		"20260522_test.png": true,
		"":                  false,
		"../image.jpg":      false,
		"nested/image.jpg":  false,
		"image..jpg":        false,
		"..hidden":          false,
		"avatar_123.webp":   true,
		"windows\\path.jpg": true,
	}
	for name, want := range tests {
		if got := isSafeFilename(name); got != want {
			t.Fatalf("isSafeFilename(%q)=%v want %v", name, got, want)
		}
	}
}

func TestSafeDeviceID(t *testing.T) {
	tests := map[string]bool{
		"CAM-01":             true,
		"EDGE_NODE:front_01": true,
		"":                   false,
		"bad/device":         false,
		"bad device":         false,
		"bad.device":         false,
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": false,
	}
	for id, want := range tests {
		if got := isSafeDeviceID(id); got != want {
			t.Fatalf("isSafeDeviceID(%q)=%v want %v", id, got, want)
		}
	}
}

func TestSupportedImageExt(t *testing.T) {
	tests := map[string]struct {
		data []byte
		ext  string
	}{
		"jpeg": {data: padded([]byte{0xff, 0xd8, 0xff, 0xdb}), ext: ".jpg"},
		"png":  {data: padded([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}), ext: ".png"},
		"webp": {data: padded([]byte{'R', 'I', 'F', 'F', 26, 0, 0, 0, 'W', 'E', 'B', 'P', 'V', 'P', '8', ' ', 14, 0, 0, 0}), ext: ".webp"},
		"text": {data: padded([]byte("not an image")), ext: ""},
		"tiny": {data: []byte{0xff, 0xd8}, ext: ""},
	}
	for name, tc := range tests {
		if got := supportedImageExt(tc.data); got != tc.ext {
			t.Fatalf("%s: supportedImageExt=%q want %q", name, got, tc.ext)
		}
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(2, time.Minute)
	if !rl.Allow("client") || !rl.Allow("client") {
		t.Fatal("first two requests should be allowed")
	}
	if rl.Allow("client") {
		t.Fatal("third request should be blocked")
	}
	if !rl.Allow("other") {
		t.Fatal("different key should have separate quota")
	}
}

func TestGetClientIP(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	if got := getClientIP(req); got != "10.0.0.2" {
		t.Fatalf("getClientIP remote=%q", got)
	}
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.2")
	if got := getClientIP(req); got != "203.0.113.10" {
		t.Fatalf("getClientIP forwarded=%q", got)
	}
}

func padded(prefix []byte) []byte {
	data := make([]byte, 512)
	copy(data, prefix)
	return data
}
