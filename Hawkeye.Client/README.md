# Hawkeye.Client

鹰眼 Hawkeye 的 Windows 端侧采集客户端。客户端使用 WPF 构建桌面界面，集成 OpenCvSharp 和 YoloDotNet，在本机运行 YOLOv8 ONNX 模型，对摄像头或视频画面进行目标检测，并将抓拍证据上传到后端。

## 功能概览

- 摄像头采集：读取本机摄像头画面，进行实时检测。
- 视频演示：加载本地视频文件，用于演示检测和上传流程。
- 本地 AI 检测：通过 `yolov8n.onnx` / `yolov8s.onnx` 执行目标检测。
- 自动抓拍：检测到目标且满足间隔条件时上传当前帧。
- 测试上传：手动上传当前帧，验证后端地址、设备 ID 和设备密钥。
- 本机配置：首次运行生成 `clientsettings.json`，保存上传地址和检测参数。

## 技术栈

- .NET 8
- WPF
- x64
- OpenCvSharp4
- OpenCvSharp4.Windows
- OpenCvSharp4.WpfExtensions
- YoloDotNet
- YOLOv8 ONNX 模型

## 目录结构

```text
Hawkeye.Client/
├── Models/
│   └── ClientSettings.cs
├── Services/
│   ├── ClientSettingsStore.cs
│   ├── DetectionResult.cs
│   ├── EvidenceUploadService.cs
│   └── YoloDetectionService.cs
├── MainWindow.xaml
├── MainWindow.xaml.cs
├── Hawkeye.Client.csproj
├── Hawkeye.Client.sln
├── yolov8n.onnx
└── yolov8s.onnx
```

## 环境要求

- Windows 10/11
- .NET 8 SDK，开发和构建时使用
- .NET 8 Desktop Runtime，非自包含发布包运行时需要
- x64 环境
- 摄像头设备，或用于演示的本地视频文件

## 构建

```powershell
cd .\Hawkeye.Client
dotnet restore .\Hawkeye.Client.sln
dotnet build .\Hawkeye.Client.sln
```

依赖由 `Hawkeye.Client.csproj` 中的 `PackageReference` 管理，不需要提交 NuGet 包、DLL、`bin/`、`obj/` 或发布目录。

## 运行配置

程序首次启动后，会在可执行文件所在目录生成 `clientsettings.json`。常用配置如下：

| 配置项 | 说明 | 示例 |
| --- | --- | --- |
| `ServerUrl` | 后端上传接口地址 | `http://127.0.0.1:8080/upload` |
| `DeviceId` | 当前采集端设备编号 | `CAM-01` |
| `DeviceKey` | 后端配置的设备密钥 | 与后端 `DEVICE_KEY` 保持一致 |
| `ConfidenceThreshold` | 检测置信度阈值 | `0.5` |
| `DetectEveryNFrames` | 检测跳帧间隔 | `3` |
| `SnapshotIntervalSeconds` | 抓拍间隔秒数 | `5` |
| `CameraIndex` | 摄像头序号 | `0` |

`clientsettings.json` 是本机运行配置，不建议提交到仓库。

## 使用流程

1. 启动后端服务，确认 `/upload` 接口可访问。
2. 启动客户端。
3. 在界面中填写后端上传地址、设备 ID、设备密钥和检测参数。
4. 点击“保存配置”。
5. 点击“启动摄像头”开始采集和检测。
6. 如需验证网络和密钥，点击“测试上传当前帧”。
7. 使用完成后点击“停止”，释放摄像头或视频资源。

## 发布

依赖目标机器安装 .NET 8 Desktop Runtime：

```powershell
dotnet publish .\Hawkeye.Client.csproj -c Release -r win-x64 --self-contained false -o .\publish\win-x64
```

不依赖目标机器运行时的自包含发布：

```powershell
dotnet publish .\Hawkeye.Client.csproj -c Release -r win-x64 --self-contained true -p:PublishSingleFile=true -o .\publish\win-x64-self-contained
```

发布目录会包含运行所需 DLL、原生依赖、ONNX 模型和程序文件。发布目录属于构建产物，不建议提交到仓库。

## 常见问题

| 问题 | 处理方法 |
| --- | --- |
| 摄像头无法启动 | 检查摄像头是否被其他程序占用，尝试修改摄像头序号。 |
| 模型加载失败 | 确认 `yolov8n.onnx` 或 `yolov8s.onnx` 在输出目录中。 |
| 上传失败 | 检查 `ServerUrl`、`DeviceKey`、后端服务状态和网络连通性。 |
| 检测卡顿 | 提高检测跳帧间隔，或改用更轻量的 `yolov8n.onnx`。 |
