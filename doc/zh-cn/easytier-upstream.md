# EasyTier 上游

本应用依赖的 EasyTier 事实。每条都针对所注明的版本验证过；内置版本变化时要重新核对。

## 发布

- 固定在 `third_party/easytier.json`（内置 tag、已测试 tag、各平台 URL 与 SHA-256）。`go run ./tools/deps check` 将其与最新稳定版比较。
- 资产名：`easytier-<os>-<arch>-<tag>.zip`，os 为 `linux|macos|windows`，arch 为 `x86_64`，以及 Linux/macOS 用 `aarch64`、Windows 用 `arm64`。每个压缩包有一个顶层目录，内含 `easytier-core`、`easytier-cli` 和 Web 控制台二进制（本应用不用）。Windows 包另含 `wintun.dll`、`Packet.dll` 与 `WinDivert64.sys`。
- 校验和：v2.6.4 没有发布校验文件；GitHub API 的 `assets[].digest` 提供 SHA-256。v2.7.0 起有 `SHA256SUMS` 资产。
- **发布的可执行文件经过 UPX 压缩。** 每次启动都要解压，导致每次 `easytier-cli` 调用约耗 250 ms CPU（v2.6.4，Linux x86_64，2026-10-16 实测）；解压后同样的调用约 3 ms，对运行中内核的真实查询（`node`、`-v peer`、`peer`、`route`）约 4 ms。`tools/deps fetch` 会用 `upx -d` 解压。
- `easytier-core` 与 `easytier-cli` 必须来自同一 release；两者的 RPC 客户端在 v2.7.0 发生了变化。
- 许可证：LGPL-3.0。

## CLI 契约（v2.6.4）

- `-o json node` 返回节点信息。其 `config` 字段是包含 `network_secret` 的完整 TOML。
- `-o json peer` 返回展示用的行（字节数和延迟是字符串）；`-v -o json peer` 返回带数值计数器的路由/连接对，流量速率依赖它。
- 单实例时 JSON 就是值本身；多实例时是 `[{instance_id, instance_name, result}]`。
- 没有运行实例：`peer` 以 `no running instances found` 失败（退出码 1）。
- RPC 不可达：第一行是 `Error: failed to get manage client`，正文包含 `Connection refused`。
- `service status` 忽略 `-o json`，输出 `Service is running`、`Service is stopped` 或 `Service is not installed`。
- `easytier-core --check-config -c FILE` 只解析配置、不启动网络；不检查端口、TUN 或权限。

## 已验证的怪癖

- 两个本地内核无法经回环握手：连接器会把源地址绑定到网卡，因此应拨本机网卡地址（`tcp://<iface-ip>:<port>`）。
- 未设置时 `--rpc-portal` 默认 15888；请始终显式指定端口。
- 没有 `--daemon` 式守护时，内核可能活着却没有运行实例；健康检查覆盖了这种情况。

## 权限

- TUN 需要 CAP_NET_ADMIN（Linux）、root（macOS，`utun`）或管理员权限加 `wintun.dll`（Windows）。`no_tun = true` 不需要任何权限：节点可经虚拟 IP 访问、可做子网代理，但不能主动连接其他节点。
- 官方 EasyTier GUI 会整体提权（pkexec、AuthorizationExecuteWithPrivileges、UAC）并在进程内运行内核。本应用保持 GUI 无特权（[run-modes.md](run-modes.md)）。
