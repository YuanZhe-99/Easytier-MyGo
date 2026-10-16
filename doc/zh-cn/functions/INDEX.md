# 函数索引

每个包一行：职责与应先阅读的声明。声明的 Go doc 注释承载 Function Explanation Layer（首句说明用途；不简单的再写输入、返回、副作用和备注）。

| 包 | 职责 | 先看 |
|---|---|---|
| `cmd/easytier-mygo` | GUI 入口 | `main` |
| `internal/gui` | GUI 状态、视图、托盘 | `App`, `App.view`, `App.connectLoop` |
| `cmd/easytier-mygo-svc` | Daemon 入口与助手子命令 | `run`, `helperCmd` |
| `cmd/easytier-mygo-ctl` | 本地 API 命令行客户端 | `main` |
| `internal/health` | 健康引擎 | `Engine.Observe`, `Policy`, `DefaultPolicy` |
| `internal/configdoc` | 原文配置文档 | `Store`, `Extract`, `Changes`, `ApplyValues`, `NewUUID` |
| `internal/coreadapter` | `easytier-cli` 封装 | `CLI`, `KindOf`, `ErrorKind` |
| `internal/telemetry` | 由计数器计算速率、曲线环形缓冲 | `Tracker.Observe`, `Ring` |
| `internal/localapi` | 本地 API 协议与服务端 | `ProtocolVersion`, `Request`, `Client.Attach`, `Authorizer` |
| `internal/profiles` | 网络存储 | `Store`, `Profile` |
| `internal/supervisor` | 每网络 worker | `Worker`, `Config`, `Preflight` |
| `internal/daemon` | 宿主、运行方式判断、API | `Run`, `Options`, `Daemon.reconcile` |
| `internal/helper` | 特权助手安装 | `Install`, `Uninstall`, `Inspect` |
| `internal/cores` | EasyTier 内核管理 | `Manager.Releases`, `Manager.Install`, `Binaries` |
| `internal/fsutil` | 原子写入 | `WriteFileAtomic`, `WriteJSON` |
| `internal/sdnotify` | systemd 通知、socket 激活 | `FromEnv`, `Listeners` |
| `internal/version` | 产品版本 | `Version` |
| `third_party` | 内嵌的 EasyTier 清单 | `EasyTier`, `EasyTierPlatformName` |
| `tools/deps` | 上游固定版本与内核下载 | `check`, `update`, `fetch` |
| `tools/checkdocs` | 文档镜像检查 | `main` |
