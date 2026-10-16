# EasyTier MyGo 重构计划（→ v0.1.0）

本文件是唯一的计划文档：完成的条目立即删除，长期契约写入 `doc/`。开工前读 `AGENTS.md` 与 `doc/en-us/architecture.md`。

## 已定决策

- **D1 运行方式**（参照 Tailscale，只授权一次）：免授权（GUI 子进程 daemon，仅 no_tun）/ 随界面（特权助手按需启动，GUI 会话结束即停网，空闲退出，不随开机）/ 后台服务（同一助手，开机运行）。GUI 永不提权；安装者为 operator。不采用 setcap，不提供 Linux 用户服务档。细节见 `doc/*/run-modes.md`。
- **D2** 标识 `com.yuanzhe.easytiermygo`，module `github.com/YuanZhe-99/Easytier-MyGo`。
- **D3** 版本唯一来源 `internal/version`；release 检查 tag==版本。
- **D4** Linux 发布 tar.gz + .deb（去 webkit 依赖、补 `libayatana-appindicator3-1`、带助手单元）；AppImage/RPM 以后再议。
- **D5** 图标暂借 EasyTier-iOS（`resources/NOTICE`），首次公开发布前核实来源或替换。
- **D6 采集**：EasyTier 发布二进制是 UPX 压缩的，解压后单次 CLI 调用 ~3 ms CPU（原 ~250 ms）。2026-10-16 实测：对运行中内核的真实查询约 4 ms CPU，1 s 刷新 <0.5% 单核 → 继续用 CLI，不做原生 RPC。
- **D8** 重构期间固定 0.1.0；用户确认后打 v0.1.0；之后每里程碑 +0.0.1。
- **D9 内核管理**：内置 core = 最新稳定版（已测试）；用户可自选官方正式版/预发布（开关）/自定义二进制，标“已测试/未测试”；从不自动下载；core 与 cli 同 release；未测试版本先 `--check-config`；启动失败可一键切回。助手档的官方内核由助手自行下载校验（不弹窗），自定义二进制每次需管理员认证。用户目录 `~/.local/share/easytier-mygo/cores/<tag>/`，助手目录 `/var/lib/easytier-mygo/cores/<tag>/`。发布二进制需 UPX 解压：Linux/Windows 随包附带官方 upx 二进制供内核管理解压下载的内核；无 upx 或解压失败时照常使用并降低刷新频率。
- **D10** 界面语言 en / zh-Hans / zh-Hant / ja（MyGo ARB），zh-Hant/ja 标注待母语者复核。

## 性能设计与目标

- daemon 每网络预分配环形缓冲（1 s 粒度，15 分钟）；GUI 经 `session.attach` 长连接接收增量推送，不再每秒拉全量；配置 revision 缓存（仅 API 写入或 mtime/size 变化时重算）。
- 采集：可见时 1 s，隐藏时 10 s；一次 `-v peer` 同时供健康与流量，`node` 低频。
- 图表：`ui/chart`，时间轴、面积+单调曲线、悬停读数、nice 刻度带滞回、1024 进制、只在新样本到达时 `Window.Update`。
- 目标（pidstat 本机 10 分钟，数字写入文档）：仪表盘打开 1 s 刷新时 daemon+GUI 合计 ≤5% 单核/网络；隐藏 ≤1%；内存平稳。

## 测试策略

只保护契约与易错逻辑，表驱动，bug 必配回归测试；不写文案/内部细节/“留证据”/崩溃矩阵/CPU 测试；`go test ./...` < 1 分钟，supervisor/daemon 加 `-race`。新代码的测试：cores（索引、资产匹配、sha256、解压）、supervisor/daemon 用假 core（测试进程重入扮演 easytier-core/cli）覆盖启停/有限恢复/应用回滚/会话断开停网/空闲退出/lifeline、localapi 授权（含附属组）、helper 路径净化与签名校验、systemd 单元 golden、少量 GUI Tester 冒烟、ring buffer 增量读取。实机：一个门控 live adapter 测试 + `scripts/live-check.sh`（隔离端口/netns 两个 dev core 端到端）。

## 里程碑

### P1 运行时、特权助手与运行方式（Linux）
1. 删除推迟功能的代码：TCP/DNS 探测（`internal/diagnostics`）、流量历史持久化（telemetry history*）、离线 profile CLI 子命令、manifest 运行时。
2. 包：`internal/profiles`（原文+元数据+持久意图）、`internal/supervisor`（每网络 worker）、`internal/daemon`（宿主、API、授权）、`internal/platform/{linux,darwin,windows}`、`internal/cores`、`internal/helper`（install/uninstall/update/install-custom-core）。
3. 动态多网络 API、RPC 端口自动分配、冲突检测、`session.attach`；ProtocolVersion → 2。
4. 宿主：用户态子进程（免授权）；特权助手（socket 激活、空闲退出、operator/附属组授权、后台档开关、配置路径净化、带加固项的 socket/service 单元）。
5. 性能设计落地；按测试策略写测试。
- 验收：助手安装只弹一次；之后启动 GUI、随界面↔后台切换、安装/切换官方内核均不弹窗；随界面档 GUI 退出或被 kill 后停网、助手空闲退出；免授权档跑 no_tun；卸载干净；daemon 侧性能达标并记录。

### P2 GUI 重设计（Linux）
- 按 `doc/*/gui.md`：侧边栏网络列表、网络页（头部卡片、流量行、实时图、信息块、节点|路由|事件）、配置（表单|TOML、保存/保存并应用）、设置（常规、运行方式档位卡与助手安装/卸载、内核管理、高级、关于）、首次向导、三态托盘（档位相关退出文案）、单实例、自启+隐藏启动、通知、四语 i18n、主题 token。
- 验收：XFCE 实机截图走查（浅/深、四语、窄窗口）；不外露内部代码；全部可滚动；性能目标达标并记录。

### P3 打包、签名与发布 CI
- `scripts/build.sh <platform>`：deps fetch → 构建 svc/ctl 进 resources → `go tool mygo build`。
- `release.yml`（tag v*/dispatch）：Linux amd64/arm64 tar.gz+deb，Windows amd64/arm64 NSIS+zip（含 wintun 等 DLL），macOS arm64/amd64 dmg（未签名）；SHA256SUMS.txt；tag==版本；GPL/LGPL 文本与 EasyTier 源码地址。
- svc 发布签名（ed25519，私钥在 GitHub secret，公钥内嵌）→ 助手凭签名自更新不弹窗；开发构建回退为认证。
- .deb：依赖修正、.desktop/AppStream、postinst 安装并启用按需 socket，`SUDO_USER` 自动成为 operator。
- 验收：本机构建并安装 Linux 产物；Linux 交叉构建 Windows 产物；actionlint。

### P4 macOS / Windows 后端（实现 + CI 编译/单测，标注未实机验证）
- macOS：LaunchDaemon（`Sockets` 按需；后台 `RunAtLoad`/`KeepAlive`），管理员认证安装一次，`LOCAL_PEERCRED`。
- Windows：服务（随界面手动启动+operator DACL；后台自动启动），命名管道+令牌授权，wintun DLL，UAC 安装一次。
- GitHub 可用后在 runner 上做助手安装冒烟测试。

### P5 0.1.0 收尾
- 升级到届时最新 MyGo/EasyTier 并复验；Linux 全量走查（三档、TUN/no_tun、自启、托盘、崩溃回收、助手签名自更新、内核切换与回退）；文档定稿；version-history 0.1.0；用户确认后打 tag 推送。

## 风险

- root 助手攻击面 → API 最小化、operator 授权、路径净化、不执行任意二进制；core 的 RPC portal 对本机用户开放（随机端口+白名单，文档说明）。
- 签名私钥管理 → GitHub secret + 轮换计划；开发构建回退认证。
- macOS socket 激活、Windows 服务 DACL 无法实机验证 → CI 冒烟 + 明确标注。
- MyGo 迭代极快 → 每次先升 MyGo，migrate-ui + Tester 回归。
- MyGo `.deb` 硬依赖 webkit → 后处理或自建 deb，向上游反馈。
- 未测试内核改变 CLI JSON 或配置格式 → 预检、采集失败降级、一键回退、adapter 宽容未知字段。
- GitHub API 未认证 60 次/小时 → 缓存 + ETag，只在手动或每日检查时请求。
