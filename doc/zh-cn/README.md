# EasyTier MyGo 文档

**本文档是代码的权威说明。** `doc/en-us/` 为源文本，`doc/zh-cn/` 逐文件镜像。计划与待办不写在这里；有进行中的多步计划时，只写在仓库根目录的 `PLAN.md` 中。

EasyTier MyGo 是 [EasyTier](https://github.com/EasyTier/EasyTier) 的原生桌面客户端，使用 Go 与 [MyGo](https://github.com/egoist/mygo) 编写。Linux 是主平台；Windows 与 macOS 后端为实验性质，已在真机上部分验证（见 [platform-notes.md](platform-notes.md)）。

| 文档 | 内容 |
|---|---|
| [architecture.md](architecture.md) | 进程、包、本地 API、跨模块规则 |
| [run-modes.md](run-modes.md) | 三种运行方式、特权助手、授权 |
| [health-monitoring.md](health-monitoring.md) | 健康状态术语、策略、有限恢复 |
| [configuration.md](configuration.md) | 原文配置、表单投影、保存与应用 |
| [gui.md](gui.md) | 窗口、侧边栏、网络小窗、编辑、设置、首次运行、节点行、文案、图表、托盘、语言 |
| [easytier-upstream.md](easytier-upstream.md) | EasyTier CLI 契约、发布资产与已验证的怪癖 |
| [platform-notes.md](platform-notes.md) | 各平台的实现与验证情况 |
| [development.md](development.md) | 环境、命令、开发循环 |
| [ci-cd.md](ci-cd.md) | 工作流、上游升级流程、发布 |
| [packaging.md](packaging.md) | 发布产物、附带的二进制、许可证、厂商标志 |
| [translation-guide.md](translation-guide.md) | 镜像规则与术语表 |
| [version-history.md](version-history.md) | 各版本及其固定的上游版本 |
| [functions/INDEX.md](functions/INDEX.md) | 各包的职责与关键声明 |

## 固定版本

| 组件 | 版本 | 唯一来源 |
|---|---|---|
| Go | 1.27.2 | `go.mod` |
| MyGo | v0.5.5 | `go.mod` |
| EasyTier（内置） | v2.6.4 | `third_party/easytier.json` |
