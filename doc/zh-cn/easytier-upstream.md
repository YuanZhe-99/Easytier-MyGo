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

## v2.7.0（预发布）中的变化

2026-10-16 用 `v2.7.0-8b7f1f01` 验证：64 位计数器（`rx_bytes`、`latency_us` 等）是带引号的字符串，NAT 类型是名称（`"Symmetric"`），默认值字段会被省略。适配器两种形式都接受（`coreadapter.Uint64`、`NatType.UnmarshalJSON`）；两个版本的夹具都在 `internal/coreadapter/testdata`。通过内核管理切换到 v2.7.0 后网络可以连接并互连。

配置方面，依据 v2.7.0 源码（`easytier-core/src/config/toml.rs`），并于 2026-10-28 实测核对（实测适配器测试通过；`--check-config` 接受这些文档）：

- `[vpn_portal_config]` 拒绝未知键。`client_cidr`（v2.6）已移除；入口由 `wireguard_listen`、可选的 `wireguard_private_key` 与 `enabled`，以及 `[[vpn_portal_config.clients]]` 中的具名客户端（`name`、带前缀的 `virtual_ip`、`groups`）组成。反过来 v2.6.4 要求 `client_cidr`，因此没有一份入口配置能同时用于两个版本。
- 新增键：`tcp_stun_servers`（顶层）、`[flags] socket_mark`（Linux，需 CAP_NET_ADMIN）、`[flags] prefer_peer_relay` 和 `[[managed_credentials]]`。v2.6.4 会忽略未知的顶层键。
- `--config-server` 必须是完整 URL（`udp://host:22020/TOKEN`）；本应用不传这个参数。
- `configdoc.VersionConflicts` 列出网络所用内核版本会拒绝或忽略的设置；编辑器会显示。应用时用该网络自己的内核运行 `--check-config`。

## EasyTier mini（v2.7.0）

由同一份源码构建的精简客户端，单独发布在 `EasyTier/easytier-mini`（LGPL-3.0）。依据 `easytier-contrib/easytier-mini` 与精简运行时（`compact_runtime_core_host_config`），于 2026-10-28 实测验证：

- 资产：`easytier-mini-<os>-<arch>-<tag>.zip`，平台名相同；文件位于压缩包根目录：只有 `easytier-mini`（Windows 另含 `wintun.dll`），约 2.4 MB，未经 UPX 压缩。GitHub API 提供每个资产的 SHA-256，也有 `SHA256SUMS`。
- 没有自己的 CLI。它的只读管理 RPC 可由同 tag 完整版的 `easytier-cli` 查询 `node`、`peer`、`-v peer`、`route` 与 `connector`；`node` 不含 `config` 字段。内核管理会连同该 CLI 一起安装 mini（`mini-<tag>`）。
- 参数：`-c/--config`、`-w/--config-server`、`--machine-id`、`--hostname`、`--secure-mode`；其他参数一律拒绝。没有 `--check-config`；无法加载的文档会使进程退出。
- 管理端口固定为 `127.0.0.1:15888`（不读取 `--rpc-portal` 与 `rpc_portal`），因此一台电脑只运行一个 mini 进程。
- 支持 `tcp://` 与 `udp://` 的监听地址、映射监听地址和对等节点，TUN 或 `no_tun`（smoltcp）、DHCP、STUN、UDP 打洞与 AES-GCM。它会静默忽略其他 URL 协议、子网代理（`proxy_network`）、出口节点（使用或充当）、SOCKS5、端口转发、WireGuard 入口、公网 IPv6 地址、MagicDNS、KCP 与 QUIC 代理、UDP 广播中继、UPnP、TCP 打洞和压缩；ChaCha20 会回退为 AES-GCM。`configdoc.StripForMini` 正好从副本中移除这些。
- v2.7.0 内核与 mini 节点可通过 TCP 直接互连。

## 已验证的怪癖

- 两个本地内核无法经回环握手：连接器会把源地址绑定到网卡，因此应拨本机网卡地址（`tcp://<iface-ip>:<port>`）。
- 未设置时 `--rpc-portal` 默认 15888；请始终显式指定端口。
- 没有 `--daemon` 式守护时，内核可能活着却没有运行实例；健康检查覆盖了这种情况。
- MagicDNS（v2.6.4 源码，`instance/dns_server`）：设置 `[flags] accept_dns = true` 时，节点会为每条带主机名的路由发布 A 记录 `<hostname>.<zone>`。zone 取 `[flags] tld_dns_zone`，默认 `et.net.`（`DEFAULT_ET_DNS_ZONE`）；主机名按原样使用，因此含空格或点的名称无法构成单个标签。

## 权限

- TUN 需要 CAP_NET_ADMIN（Linux）、root（macOS，`utun`）或管理员权限加 `wintun.dll`（Windows）。`no_tun = true` 不需要任何权限：节点可经虚拟 IP 访问、可做子网代理，但不能主动连接其他节点。
- 官方 EasyTier GUI 会整体提权（pkexec、AuthorizationExecuteWithPrivileges、UAC）并在进程内运行内核。本应用保持 GUI 无特权（[run-modes.md](run-modes.md)）。
