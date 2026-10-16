# 平台说明

| 平台 | 状态 |
|---|---|
| Linux（systemd） | 主平台。新 daemon、运行方式与助手已实现并验证（见下）；GUI 正在重建。 |
| macOS | 可构建。助手（launchd）与运行方式尚未实现；未在真机上验证任何内容。 |
| Windows | 可构建。助手（服务）与运行方式尚未实现；未在真机上验证任何内容。 |

## Linux

- 开发机：Debian 13，X11 上的 XFCE，有 StatusNotifier 托盘宿主，有 pkexec。
- 托盘使用 `libayatana-appindicator3`（回退到 `libappindicator3`）；没有它时应用在无托盘状态下运行。
- MyGo 的登录项会写入 XDG 自启动条目。

已在开发机上验证（2026-10-16，EasyTier v2.6.4，隔离端口）：

| 检查 | 结果 |
|---|---|
| 用户态 daemon，两个 `no_tun` 网络 | 会话连接时启动、会话结束时停止；意图保留 |
| Lifeline | 管道 EOF 时停止内核与 daemon |
| 崩溃恢复 | `kill -9` 内核后由有限恢复重启 |
| 应用回滚 | 内核退出的 revision 会回到之前运行的 revision |
| 安装助手（pkexec） | 二进制、组、operator、单元；socket 活动，服务按需启动 |
| 助手运行 TUN | 两个 TUN 网络（`etm0`、`etm1`）以 root 运行、互连、可 ping |
| 随界面 | 会话结束时停止内核并移除设备；空闲 60 s 退出；socket 重新激活 |
| 后台方式 | 服务设为开机启动；无会话时网络运行；关闭后回到随界面（未测试重启） |

## macOS 与 Windows

计划中的助手后端：launchd 守护进程（经 `Sockets` 按需启动，后台方式用 `RunAtLoad`/`KeepAlive`）和 Windows 服务（手动启动加 operator DACL，后台方式为自动启动）。在真机验证之前，关于它们的一切都只是设计，不是结果。

## 本机注意

开发机以 root 运行着生产 EasyTier 内核（RPC 15888、监听 11010、设备 `easytier0`）。绝不触碰它。开发用内核使用显式端口：RPC 从 25888 起，监听从 21110 起。
