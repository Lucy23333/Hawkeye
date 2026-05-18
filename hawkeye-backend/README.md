# 🦅 鹰眼 (Hawkeye) - 智能视觉安防指挥系统

> **Version:** V11.4 (Final Fusion)  
> **Status:** Stable / MVP  
> **Author:** [sia/ID]

![Badge](https://img.shields.io/badge/Language-Go%20%7C%20C%23-blue)
![Badge](https://img.shields.io/badge/AI-YOLOv8%20%2B%20QwenVL-green)
![Badge](https://img.shields.io/badge/Database-MySQL%20(Docker)-orange)
![Badge](https://img.shields.io/badge/Architecture-IoT%20Edge%20Cloud-purple)

## 📖 项目简介 (Introduction)

**鹰眼 (Hawkeye)** 是一套基于 **[端-边-云]** 架构的分布式智能安防系统。
它不仅仅是一个监控工具，而是一个集成了**边缘计算、实时视觉识别、云端数据存储、大数据可视化**以及**生成式 AI (AIGC) 分析**的完整物联网解决方案。

系统支持 Windows 客户端实时抓拍、移动端网页监控，并通过 Go 语言构建的高性能后端进行统一调度与管理。

---

## ✨ 核心功能 (Features)

### 1. 👁️ 全平台视觉采集 (Multi-Platform Vision)
* **Windows 边缘计算端：** 基于 C# WPF + OpenCV，本地运行 YOLOv8 模型，实现**毫秒级**人形检测。只有识别到目标时才上传，节省 90% 带宽。
* **Mobile Web 移动端：** 任何闲置手机通过浏览器即可变身监控探头，支持自动延时摄影上传。

### 2. 🧠 双重 AI 大脑 (Dual AI Core)
* **实时检测 (Edge):** YOLOv8s 本地推理，实时框选入侵目标。
* **深度分析 (Cloud):** 接入 **Qwen-VL (通义千问视觉大模型)**，可对抓拍图片进行语义理解（如：“图中有个穿红衣服的人在奔跑”）。

### 3. 📊 赛博朋克指挥中心 (Command Dashboard)
* **可视化大屏：** 集成 ECharts 动态图表，实时展示入侵频率与时间分布。
* **全息日志：** 滚动式终端日志，实时反馈系统运行状态。
* **动态交互：** 采用粒子特效引导页与登录页，科技感拉满。

### 4. 🛡️ 企业级数据管理 (Data Management)
* **云端存储：** 图片文件与元数据分离存储（Linux 文件系统 + MySQL）。
* **配置中心：** 支持网页端热更新 AI 配置（Key/Model），无需重启服务。
* **安全验证：** 完整的登录/注销机制，保护监控数据。

---

## 🛠️ 技术栈 (Tech Stack)

| 模块 | 技术选型 | 说明 |
| :--- | :--- | :--- |
| **后端 (Server)** | **Go (Golang)** | 高并发 HTTP 服务，原生 `net/http` |
| **前端 (Web)** | **HTML5 / CSS3 / JS** | 赛博朋克 UI，ECharts 图表，Fetch API |
| **客户端 (Client)** | **C# / WPF** | .NET 6.0，OpenCvSharp4，YoloDotNet |
| **数据库 (DB)** | **MySQL 8.0** | Docker 容器化部署 |
| **AI 模型** | **YOLOv8 + Qwen-VL** | 目标检测 + 多模态图文理解 |
| **部署 (DevOps)** | **Docker / cpolar** | 容器化数据库 + 内网穿透 |

---

## 🚀 快速启动 (Quick Start)

### 1. 环境准备
确保 Ubuntu 服务器已安装 Docker 和 Go 环境。

### 2. 启动数据库 (MySQL)
```bash
# 启动 Docker 容器
sudo docker start hawkeye-db

# (可选) 重置/清空所有数据
sudo docker exec -it hawkeye-db mysql -u root -proot -e "TRUNCATE TABLE hawkeye.events;"

##启动后端服务,服务默认运行在 :8080 端口。
# 进入项目目录
cd ~/hawkeye-backend

# 启动主程序
go run main.go

开启公网访问 (cpolar)
# 获取公网链接
cpolar http 8080

#启动采集端
PC端： 运行 Hawkeye.Client.exe。

手机端： 访问公网链接 /camera。


系统首次启动会自动生成 config.json。 你可以通过访问 Dashboard -> Settings 进行在线配置：

AI Endpoint: https://api.siliconflow.cn/v1/chat/completions (硅基流动)

AI Model: Qwen/Qwen2-VL-72B-Instruct

Admin: 默认账号密码均为 admin（建议首次登录后修改）

也可以使用环境变量覆盖配置（避免明文落盘密钥）：

AI_ENDPOINT, AI_KEY, AI_MODEL, ADMIN_USER, ADMIN_PASS, DING_WEBHOOK, DEVICE_KEY, ALERT_KEYWORDS


未来规划 (Roadmap)
[ ] V12.0: 引入 WebSocket 实现毫秒级画面同步。

[ ] V13.0: 增加人脸识别库，区分熟人与陌生人。

[ ] V14.0: 封装 Docker 镜像，实现一键 docker-compose up 部署。
Copyright © 2025 Hawkeye Project. All Rights Reserved.