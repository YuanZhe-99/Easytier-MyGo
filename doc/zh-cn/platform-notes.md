# 平台说明

| 平台 | 状态 |
|---|---|
| Linux（systemd） | 主平台。继承来的 daemon 与 GUI 可运行；运行方式、助手与新 GUI 正在重建。 |
| macOS | 可构建。助手（launchd）与运行方式尚未实现；未在真机上验证任何内容。 |
| Windows | 可构建。助手（服务）与运行方式尚未实现；未在真机上验证任何内容。 |

## Linux

- 开发机：Debian 13，X11 上的 XFCE，有 StatusNotifier 托盘宿主，有 pkexec。
- 托盘使用 `libayatana-appindicator3`（回退到 `libappindicator3`）；没有它时应用在无托盘状态下运行。
- MyGo 的登录项会写入 XDG 自启动条目。

## macOS 与 Windows

计划中的助手后端：launchd 守护进程（经 `Sockets` 按需启动，后台方式用 `RunAtLoad`/`KeepAlive`）和 Windows 服务（手动启动加 operator DACL，后台方式为自动启动）。在真机验证之前，关于它们的一切都只是设计，不是结果。

## 本机注意

开发机以 root 运行着生产 EasyTier 内核（RPC 15888、监听 11010、设备 `easytier0`）。绝不触碰它。开发用内核使用显式端口：RPC 从 25888 起，监听从 21110 起。
