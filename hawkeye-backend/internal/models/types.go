package models

import "time"

// 全局时区设置
var CstZone = time.FixedZone("CST", 8*3600)

// Config 配置结构体
type Config struct {
	AIEndpoint string `json:"ai_endpoint"`
	AIKey      string `json:"ai_key"`
	AIModel    string `json:"ai_model"`
	AdminUser  string `json:"admin_user"`
	AdminPass  string `json:"admin_pass"`
	Avatar     string `json:"avatar"`
}

// Event 数据库事件模型
type Event struct {
	ID          int    `json:"ID"`
	Filename    string `json:"Filename"`
	CaptureTime string `json:"CaptureTime"`
	AIAnalysis  string `json:"AIAnalysis"`
	DeviceID    string `json:"DeviceID"`
}

// APIResponse 通用列表响应
type APIResponse struct {
	Count  int     `json:"count"`
	Events []Event `json:"events"`
}

// DeviceInfo 设备信息
type DeviceInfo struct {
	ID         string `json:"id"`
	LastImage  string `json:"last_image"`
	LastActive string `json:"last_active"`
}

// AnalysisResponse AI分析响应
type AnalysisResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

// DingMsg 钉钉消息结构
type DingMsg struct {
	MsgType  string `json:"msgtype"`
	Markdown struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	} `json:"markdown"`
}