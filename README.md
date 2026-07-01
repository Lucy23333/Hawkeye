# 鹰眼 Hawkeye

基于端边云协同的智能安防预警系统。项目由 Go 后端/Web 管理端和 Windows WPF 采集客户端组成，面向园区、校园、工厂、社区和家庭等安防场景，提供设备接入、实时采集、端侧目标检测、证据上传、AI 语义分析和事件管理能力。

## 项目结构

```text
Project-Hawkeye/
├── hawkeye-backend/      # Go 后端、Web 管理端、移动采集页、数据库迁移
├── Hawkeye.Client/       # Windows WPF 采集客户端，集成 YOLOv8 ONNX 检测
├── 鹰眼 (...).docx       # 软著用户手册及相关文档
└── .gitignore
```

## 核心能力

- 端侧检测：Windows 客户端使用 YOLOv8 ONNX 模型进行本地目标检测。
- 证据上传：客户端或移动端将抓拍图片上传到后端 `/upload` 接口。
- Web 管理：后端提供登录、Dashboard、设备管理、事件记录、配置管理等页面。
- AI 分析：后端可调用多模态大模型接口，对事件图片生成自然语言分析结果。
- 设备安全：通过 `DeviceKey` / `X-Device-Key` 控制设备上传和数据订阅。
- 数据留存：事件元数据写入 MySQL，图片文件保存到 `uploads/`。

## 技术栈

| 模块 | 技术 |
| --- | --- |
| 后端 | Go 1.18、net/http、MySQL |
| Web 管理端 | HTML、CSS、JavaScript、服务端模板 |
| 数据库 | MySQL 8.0 |
| 容器 | Docker、Docker Compose |
| Windows 客户端 | .NET 8 WPF、OpenCvSharp、YoloDotNet |
| AI 模型 | YOLOv8 ONNX、本地目标检测；Qwen-VL 等多模态模型用于云端分析 |

## 快速启动

### 1. 启动后端

```powershell
cd .\hawkeye-backend
copy .env.example .env
# 编辑 .env：至少修改 ADMIN_PASS、DEVICE_KEY、AI_KEY
docker compose up -d --build
```

服务默认地址：

```text
http://localhost:8080
```

常用入口：

- `/login`：登录页
- `/app`：Web 管理端
- `/camera`：移动端采集页
- `/health`：健康检查

### 2. 启动 Windows 采集端

```powershell
cd .\Hawkeye.Client
dotnet restore .\Hawkeye.Client.sln
dotnet build .\Hawkeye.Client.sln
```

运行客户端后配置：

- 后端上传地址：`http://服务器地址:8080/upload`
- 设备 ID：例如 `CAM-01`
- 设备密钥：与后端 `DEVICE_KEY` 一致

## 子项目说明

详细部署和使用说明见：

- [hawkeye-backend/README.md](hawkeye-backend/README.md)
- [Hawkeye.Client/README.md](Hawkeye.Client/README.md)

## 文档

根目录包含软著用户手册和技术复盘文档，其中修订版用户手册已按实际项目功能整理，可用于软著材料、交付说明和培训说明。

## 注意事项

- 不要提交 `.env`、`config.json` 中的真实密钥和生产账号。
- `uploads/` 保存运行期图片和头像，生产环境应定期备份或清理。
- 客户端模型文件较大，发布时需要确认 ONNX 模型已复制到输出目录。
