# 架构

## 进程

| 进程 | 二进制 | 运行身份 | 职责 |
|---|---|---|---|
| GUI | `easytier-mygo` | 当前用户，永不提权 | 窗口、托盘、设置。是 daemon 的客户端，自身不持有网络状态。 |
| Daemon | `easytier-mygo-svc` | 当前用户（免授权方式）或 root/SYSTEM（特权助手） | 管理网络配置，为每个运行中的网络启动一个 `easytier-core`，监测健康，执行有限恢复，提供本地 API。 |
| 内核 | `easytier-core` + `easytier-cli` | 继承 daemon | EasyTier 网络本身及其管理 CLI，始终是同一 release 的配套组合。 |
| 控制 CLI | `easytier-mygo-ctl` | 当前用户 | 可脚本化的本地 API 客户端。 |

Daemon 按运行方式（[run-modes.md](run-modes.md)）运行在两种权限环境之一：GUI 的用户态子进程，或在系统中一次性注册的特权助手。两者的 API 与行为相同，只有授权、路径和生命周期不同。

## 包

| 包 | 职责 |
|---|---|
| `internal/health` | 无副作用的健康引擎：判定、归因、有限恢复决策。 |
| `internal/configdoc` | 原文 TOML 文档：revision、带冲突检查的原子保存、备份、无损表单投影。 |
| `internal/coreadapter` | 对 `easytier-cli` 输出的类型化、有界封装与错误分类。 |
| `internal/telemetry` | 由计数器计算速率，并保存带序号的曲线环形缓冲；缺口与重置保持可见。 |
| `internal/localapi` | 经本地 socket 传输、带版本的按行 JSON 协议，会话推送，按对端凭据授权。 |
| `internal/profiles` | 网络存储：原文文档、元数据、运行意图、冻结的已应用 revision。 |
| `internal/supervisor` | 每个网络一个 worker：内核进程、检查、有限恢复、带回滚的应用、流量。 |
| `internal/daemon` | 宿主：worker、运行方式判断、会话、API 处理。 |
| `internal/helper` | 安装、卸载与检查特权助手（Linux：systemd）。 |
| `internal/sdnotify` | systemd 通知与 socket 激活。 |
| `internal/fsutil` | 原子文件写入与 JSON 文档。 |
| `internal/app` | GUI（正在重建；目前只是占位窗口）。 |
| `internal/version` | 唯一的产品版本。 |
| `third_party` | 内嵌的 EasyTier 发布清单。 |
| `tools/deps` | 维护上游固定版本、下载内置内核。 |

网络在用户希望连接**并且**运行方式允许时才运行：有 GUI 会话连接，或助手处于后台方式。每个运行中的网络有自己的内核，管理端口从 25888 起显式分配。应用已保存的文档时先运行内核的 `--check-config`，再用冻结副本重启内核；如果新 revision 在确认窗口（30 s）内失败，worker 会回到之前运行的 revision。

会话正在显示该网络（或其内核尚不健康）时每秒检查一次，否则每 10 s 一次；一次 `node` 与一次 `-v peer` 查询同时供健康判定与流量使用。

## 本地 API

- 传输：Unix 域 socket（Windows 命名管道在计划中）；按行 JSON，带 `version` 字段。每个连接一个请求，会话（`session.attach`）除外：它保持连接并接收推送（网络状态、新的流量点、新事件），最多每 500 ms 一次。错误带代码（`not-found`、`conflict`、`invalid-config`、`needs-helper` 等）。
- 双方都检查 `localapi.ProtocolVersion`。枚举以名称传输。改变字段含义必须升级版本。
- 授权来自操作系统提供的对端凭据，绝不来自请求内容。只读与写入两级保护的是本应用的控制入口。
- 内核自己的 `--rpc-portal`（回环 TCP）是另一个入口，本机用户都能访问；本应用不声称将其关闭。

## 跨模块规则

- **健康语义是产品契约**——见 [health-monitoring.md](health-monitoring.md)。
- **绝不把陈旧数据当作当前状态。** 每个快照都带采样时间；GUI 会标出过期数据和断开的连接。
- **密钥绝不外传。** `easytier-cli node` 返回含 `network_secret` 的完整 TOML；在缓存、记录日志或经 API 发送之前就被清空。事件详情在存储前脱敏。
- **配置是原文文档**——见 [configuration.md](configuration.md)。
- **一个内核一个实例。** 多实例的 CLI 输出报告为 `ErrMultiInstance`，绝不误解析。
