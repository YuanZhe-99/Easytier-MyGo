# 函数索引

每个包一行：职责与应先阅读的声明。声明的 Go doc 注释承载 Function Explanation Layer（首句说明用途；不简单的再写输入、返回、副作用和备注）。

| 包 | 职责 | 先看 |
|---|---|---|
| `cmd/easytier-mygo` | GUI 入口 | `main` |
| `cmd/easytier-mygo-svc` | Daemon 入口与服务子命令 | `main`, `runDaemon` |
| `cmd/easytier-mygo-ctl` | 本地 API 命令行客户端 | `main` |
| `internal/health` | 健康引擎 | `Engine.Observe`, `Policy`, `DefaultPolicy` |
| `internal/configdoc` | 原文配置文档 | `Store`, `Extract`, `Changes`, `ApplyValues` |
| `internal/coreadapter` | `easytier-cli` 封装 | `CLI`, `KindOf`, `ErrorKind` |
| `internal/telemetry` | 由计数器计算速率 | `Tracker.Observe`, `Point` |
| `internal/localapi` | 本地 API 协议与服务端 | `ProtocolVersion`, `Client`, `Authorizer` |
| `internal/daemon` | 监管与 API 处理 | `RunService`, `Daemon` |
| `internal/platformservice` | systemd 单元 | `Install`, `Uninstall` |
| `internal/sdnotify` | systemd 通知 | `FromEnv`, `Notifier.Watchdog` |
| `internal/app` | GUI 视图与托盘 | `App`, `App.View` |
| `internal/version` | 产品版本 | `Version` |
| `third_party` | 内嵌的 EasyTier 清单 | `EasyTier`, `EasyTierPlatformName` |
| `tools/deps` | 上游固定版本与内核下载 | `check`, `update`, `fetch` |
| `tools/checkdocs` | 文档镜像检查 | `main` |
