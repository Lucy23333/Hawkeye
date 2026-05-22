# Hawkeye.Client

## 依赖处理

客户端依赖通过 `Hawkeye.Client.csproj` 里的 `PackageReference` 管理，不需要把 NuGet 包、DLL、`bin/`、`obj/` 或发布目录提交到仓库。

首次拉取项目后执行：

```powershell
dotnet restore .\Hawkeye.Client.sln
dotnet build .\Hawkeye.Client.sln
```

## 在项目外运行

如果要把客户端放到项目目录外运行，使用发布目录，不要直接复制源码目录。

依赖本机已安装 .NET 8 Desktop Runtime 的发布方式：

```powershell
dotnet publish .\Hawkeye.Client.csproj -c Release -r win-x64 --self-contained false -o .\publish\win-x64
```

不依赖目标机器安装运行时的发布方式：

```powershell
dotnet publish .\Hawkeye.Client.csproj -c Release -r win-x64 --self-contained true -p:PublishSingleFile=true -o .\publish\win-x64-self-contained
```

发布目录会包含运行所需 DLL、原生依赖、ONNX 模型和程序文件。这个目录体积较大，属于构建产物，已经在根目录 `.gitignore` 中忽略。

## 运行配置

程序首次启动会在可执行文件所在目录生成 `clientsettings.json`。常用配置：

- `ServerUrl`: Ubuntu 后端上传地址，例如 `http://192.168.153.131:8080/upload`
- `DeviceId`: 设备编号，例如 `CAM-01`
- `DeviceKey`: 后端 `config.json` 或控制台设置页中的 `device_key`

`clientsettings.json` 是本机配置，不建议提交到仓库。
