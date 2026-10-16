# 平台说明

| 平台 | 状态 |
|---|---|
| Linux（systemd） | 主平台。新 daemon、运行方式与助手已实现并验证（见下）；GUI 正在重建。 |
| macOS | 已实现；CI 冒烟测试（用户态 daemon 与助手，`no_tun`）在 GitHub runner 上通过；用户态 daemon、GUI 启动的 daemon 子进程与 LaunchDaemon 助手（用 `sudo` 安装，`no_tun` 与 TUN）已在真机验证（见下）；未经真机验证：经 `osascript` 的管理员授权、签名自更新。 |
| Windows（x64、ARM64） | 已实现；CI 冒烟测试（用户态 daemon 与助手，`no_tun`）在 GitHub runner 上通过；未经真机验证：用户态 daemon、服务助手、经 PowerShell 的 UAC、AF_UNIX 上按 SID 授权、内核的作业对象、签名自更新。 |

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
| 内核管理（用户态 daemon） | 下载 v2.7.0（校验 SHA-256），把运行中的网络切换过去再切回，删除 |
| 内核管理（助手） | 助手自行下载 v2.7.0；TUN 网络在其上运行并切回内置内核 |
| GUI（真实桌面，XFCE） | 实时数据、图表、节点、托盘已注册；kill -9 或 SIGTERM 结束 GUI 时其 daemon 与内核随之停止 |
| CPU，两个 `no_tun` 网络，120 s | 仪表盘可见：GUI 3.5 %、daemon 1.3 %、内核 0.9 %（合计单核 5.7 %）。隐藏到托盘：GUI 0.4 %、daemon 0.4 %、内核 0.6 %（1.4 %）。GUI 常驻内存 140–160 MB，daemon 18 MB |
| GUI 运行方式切换 | 免授权 → 随界面无需弹窗；网络复制到助手并连接；退出 GUI 后网络停止 |
| GUI 后台服务 | 免授权 → 后台：网络迁移到助手，服务设为开机启动；退出 GUI 后网络继续运行；后台 → 免授权：助手中的网络停止、取消开机启动，并在用户态 daemon 中连接；切换过程中没有端口冲突 |
| 登录时打开 | 开关会写入 `~/.config/autostart/EasyTier-MyGo.desktop`；登录启动时隐藏在托盘中并连接网络 |
| 安装包 | 用 `install.sh` 把 tar.gz 安装到测试用的 home 后，可用内置内核运行网络；`.deb` 只依赖 GTK 3 与托盘库 |
| 冒烟脚本 | `scripts/smoke.sh user` 与 `helper` 在本机通过 |
| 助手自更新 | 签名的构建包无需弹窗即被接受（替换二进制并重启助手）；未签名或被篡改的构建包被拒绝；`release.yml` 用 GitHub secret 签名的构建包同样被接受 |
| GUI 助手按钮 | 经真实 polkit 对话框（pkexec）卸载与安装，operator 被记录 |

## macOS 与 Windows

- **macOS：** 助手是 LaunchDaemon `com.yuanzhe.easytiermygo.helper`（`/Library/LaunchDaemons`，二进制在 `/Library/Application Support/EasyTier MyGo/bin`），始终加载（`RunAtLoad`/`KeepAlive`）：launchd 的 socket 激活需要 C API，因此“随界面”由 daemon 保证（除非开启后台方式，否则只有会话连接时才运行网络）。Socket 为 `/var/run/easytier-mygo/ctl.sock`；用 `LOCAL_PEERCRED` 识别调用者。由于 `osascript` 不带 `SUDO_UID`，operator 通过 `--operator` 传入。
- **Windows：** 助手是自动启动的服务 `EasyTierMyGoHelper`（二进制在 `%ProgramFiles%\EasyTier MyGo\helper`，状态在 `%ProgramData%\EasyTier MyGo\state`，用 `icacls` 限制权限），失败时重启。本地 API 为 AF_UNIX socket；通过 `SIO_AF_UNIX_GETPEERPID` 与其令牌（用户 SID、Administrators 成员身份）识别调用者。用户态 daemon 监视 GUI 进程（`--parent-pid`），因为管道无法传给子进程；内核运行在关闭即终止的作业对象中。自更新会先把运行中的可执行文件改名移开，然后退出，由服务管理器重启。

在真机验证之前，这些只是可编译并通过 vet 的设计，而不是结果。
目前已在 Windows 11 ARM64（Go 1.27.2，无 C 编译器）上验证：本地检查全部通过；`scripts/smoke.sh user` 通过用户态 daemon 在附带的 v2.6.4 内核上运行 `no_tun` 网络，并通过 API 关闭。通过 UAC 安装和移除的特权助手，在会话连接时（`ctl watch`）为非管理员 operator 运行同一网络；卸载会移除服务、二进制、状态以及 `%ProgramFiles%` 与 `%ProgramData%` 下的 `EasyTier MyGo` 文件夹。

已在 macOS 27.0.1、Apple M1 上验证（2026-10-16，Homebrew 安装的 Go 1.27.2，EasyTier v2.6.4）：

| 检查 | 结果 |
|---|---|
| 本地检查 | 通过，包括 `CGO_ENABLED=0` |
| 冒烟脚本 | `scripts/smoke.sh user` 通过 |
| 用户态 daemon，两个 `no_tun` 网络 | 经网卡地址互为对等节点并连接；live adapter 测试在两个内核上通过 |
| 崩溃恢复 | `kill -9` 内核 → 由有界恢复重启，回到健康 |
| 生命线 | 管道 EOF 停止内核与 daemon；socket 被移除 |
| 软件包 | `scripts/build.sh` 构建出 `.app` 与 `.dmg` |
| GUI 的 daemon 子进程 | 无特权方式下，`.app` 从其资源目录启动 daemon（socket 权限 0600，位于临时目录下）；`kill -9` GUI 会停止 daemon 及其内核 |
| 助手安装（`sudo … --operator`） | LaunchDaemon 已加载，二进制归 root 所有，operator 以非 root 用户获得写权限 |
| 助手，随界面 | 已连接的网络在没有会话时保持停止，有会话时以 root 运行；会话全部结束后所有内核停止 |
| 助手与 TUN | 两个 TUN 网络获得带地址的 `utun` 设备并互为对等节点；停止后设备消失 |
| 助手卸载 `--purge` | 移除 LaunchDaemon、二进制、状态、`EasyTier MyGo` 文件夹与 socket 目录 |
| Daemon 测试 | `internal/daemon` 测试也在 macOS 上运行（以前仅限 Linux） |

macOS 上未验证：GUI 安装按钮触发的 `osascript` 授权、助手自更新，以及 GUI 外观（未截图）。

## 已知限制

- EasyTier 内核自带的 RPC portal（回环 TCP，端口从 25888 起）对本机所有用户可达，本机用户可以通过 `easytier-cli` 查询或修改运行中的内核。本应用的本地 API 受保护，内核的 portal 不受保护。目标场景是单用户桌面。
- 特权助手以 root/SYSTEM 运行内核；其 API 仅限 operator 与 `easytier-mygo` 组，并拒绝路径类配置项。
- Windows 只验证过 `no_tun` 网络；WinTun 未测试。macOS 的 TUN（utun）已通过助手验证。
- 标为未测试的内核可能改变 CLI 输出或配置格式；应用会降级为“无法读取状态”而不重启任何东西，也可以重新选回之前的内核。
- 未认证的 GitHub API 请求每小时限 60 次；发布列表按 ETag 缓存，只在请求时或每日检查时获取。
- MyGo 生成的 `.deb` 对所有应用都声明依赖 WebKitGTK；`scripts/build.sh` 会去掉该依赖。
