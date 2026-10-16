# 运行方式

在所有平台上创建 TUN 设备都需要管理员权限。EasyTier MyGo 采用 Tailscale 的做法：管理员授权只在安装特权助手时进行**一次**，之后启动 GUI 不再要求授权。GUI 本身永不提权。

## 三种方式

| 方式 | 要求 | 网络何时运行 | Linux | macOS | Windows |
|---|---|---|---|---|---|
| 免授权 | 无 | GUI 运行期间；仅限 `no_tun` 网络 | GUI 以子进程启动用户态 daemon | 同左 | 同左 |
| 随界面 | 安装一次特权助手 | GUI 会话存在期间。GUI 退出或崩溃时网络停止，空闲的助手随后退出。开机不运行任何东西。 | systemd socket 激活的系统单元（不设开机启动） | 经 `Sockets` 按需启动的 launchd 守护进程 | 手动启动型服务，DACL 允许 operator 启动 |
| 后台服务 | 同一个助手；从随界面切换无需再次授权 | 开机即运行，与 GUI 无关 | 同一单元启用到 `multi-user.target` | `RunAtLoad` + `KeepAlive` | 自动启动 |

## 授权

- 安装或卸载助手时询问一次：pkexec（Linux）、管理员密码（macOS）、UAC（Windows）。执行安装的用户成为 **operator**。
- 助手的 socket 接受 operator 及 `easytier-mygo` 组成员（含附属组）的写入，拒绝其他用户。
- 只有以下情况会弹出授权：安装或卸载助手、安装自定义内核二进制、用未签名构建更新助手。启动 GUI 时绝不弹出。

## 会话绑定

GUI 与助手保持一条长连接会话。会话关闭且当前为随界面方式时，助手停止网络；没有会话也没有运行中的网络时，助手空闲一段时间后退出，下次连接时由 systemd/launchd 重新拉起。

## 助手的安全规则

助手以 root/SYSTEM 运行内核，因此：

- 路径类配置项（日志目录）被强制放在助手状态目录下；
- 官方内核由助手自行下载并校验 SHA-256，绝不从用户可写的文件复制；
- 自定义内核二进制每次都需要管理员授权；
- 助手二进制位于 root 拥有的位置；只会用带有效发布签名的构建替换自身。

## 与其他 EasyTier 安装隔离

单元、socket 与状态名都使用 `easytier-mygo`；TUN 设备名使用 `etm` 前缀；daemon 始终显式传入 `--rpc-portal`。绝不假定 EasyTier 默认值（RPC 15888、监听 11010、设备 `easytier0`）。

## 当前实现

Linux：用户态 daemon（`easytier-mygo-svc run --host user --lifeline-fd N`）、助手（`run --host helper`，socket 激活）以及 `easytier-mygo-svc helper install|uninstall|status` 已实现；GUI 流程随新界面提供。`helper install` 必须以 root 运行（GUI 使用 pkexec），并把 `PKEXEC_UID`/`SUDO_UID` 对应的用户记为 operator。按签名自更新助手以及 macOS/Windows 后端尚未实现。验证情况记录在 [platform-notes.md](platform-notes.md)。
