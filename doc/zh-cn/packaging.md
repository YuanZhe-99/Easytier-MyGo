# 打包

`scripts/build.sh [goos/goarch …]` 获取内核，把 daemon 与控制 CLI 构建进平台资源目录，在设置了 `EASYTIER_MYGO_SIGNING_KEY` 时为构建包签名，运行 `go tool mygo build`，并去掉 `.deb` 中 MyGo 默认的 WebKitGTK 依赖。`go tool mygo build` 生成各平台产物：Linux 上是 `.tar.gz`（附 `install.sh`，按用户安装）和 `.deb`；Windows 上是 `.exe` 与 NSIS 安装包；macOS 上是 `.app` 与 `.dmg`（需要 macOS 主机）。设置了 `MYGO_UPDATER_PRIVATE_KEY` 时，还会写出各平台签名的更新文件（[ci-cd.md](ci-cd.md)）；其更新说明取自 `doc/en-us/version-history.md` 中该版本的一节。

## macOS 代码签名

macOS 应用用维护者的 Apple Development 证书（Team ID `982C6JDNCS`）签名，维护者的所有 macOS 应用共用这张证书。`scripts/build.sh` 把 `MYGO_SIGN` 传给 `mygo build -sign`，由它以 hardened runtime 和安全时间戳为应用、其资源中的每个可执行文件以及磁盘映像签名：

```bash
MYGO_SIGN=<certificate SHA-1> scripts/build.sh darwin/arm64
codesign -dvv "build/darwin-arm64/EasyTier MyGo.app"   # TeamIdentifier=982C6JDNCS
```

签名身份用证书的 SHA-1 指定（`security find-identity -p codesigning`）；签名用的 Mac 的钥匙串里还需要 Apple 的 WWDR G3 中间证书。有了 Team ID，应用的指定要求（designated requirement：标识符、Apple 根证书、证书）在每次构建中都相同，macOS 因此在重新构建和更新后保留隐私授权；ad hoc 签名（不设 `MYGO_SIGN`）只绑定某一次构建的哈希。应用不需要任何 entitlement：它的可执行文件是 Go 和 Rust 程序，不加载 macOS 以外的库，库验证没有可拒绝的对象。应用没有经过公证，因此下载的副本会被 Gatekeeper 拦截，直到移除其隔离属性（[platform-notes.md](platform-notes.md)）。

macOS 签名会改写附带的可执行文件，因此特权助手的构建包清单（`EASYTIER_MYGO_SIGNING_KEY`，在 `mygo build` 之前根据文件生成）与它们不再一致；在 macOS 上更新助手需要管理员批准。

## 附带的二进制

`resources/<goos>-<goarch>/` 下的文件随应用一起分发，运行时通过 `mygo.App.Path(mygo.PathResources)` 找到。`go run ./tools/deps fetch -platform <goos>-<goarch>` 下载固定的 EasyTier 压缩包、校验 SHA-256，并把内核与 CLI（去掉 UPX）解压到 `resources/<goos>-<goarch>/bin`，同时放入固定版本的 `upx`（`third_party/upx.json`；仅 Linux 与 Windows），供内核管理处理下载的内核。这些目录是构建输入，不提交到仓库。

## 许可证

EasyTier MyGo 采用 GPL-3.0。内置的 EasyTier 二进制为 LGPL-3.0：每个分发包都附带其许可证文本和 `third_party/easytier.json` 中记录的源码地址。图标来自 EasyTier-iOS（GPL-3.0-or-later）；见 `resources/NOTICE`。内置的 `upx` 为 GPL-2.0-or-later 并带 UPX 例外条款，其许可证以 `upx-COPYING` 随附。界面图标来自 Lucide（ISC）。厂商标志不属于 GPL-3.0 源码；见下文。

## 厂商标志

节点列表中的厂商标志（[gui.md](gui.md) 的“节点行”）是 `internal/gui/assets/logos/` 下的 SVG 文件，构建时嵌入界面程序；应用从不下载它们。该目录中的 `SOURCES.md` 列出每个文件的品牌、来源、许可证和商标所有者，`resources/NOTICE` 为分发包作了概述。大多数文件来自 [Simple Icons](https://simpleicons.org/)（CC0 1.0），其余来自 Wikimedia Commons（公有领域的文字或简单几何标志），或来源未记录，`SOURCES.md` 中如实注明。

添加或更新标志：

1. 从固定版本的 `simple-icons` npm 包下载 `icons/<slug>.svg`，例如 `https://cdn.jsdelivr.net/npm/simple-icons@<version>/icons/<slug>.svg`，或从 Wikimedia Commons 下载公有领域文件。优先使用 Simple Icons：其文件为 CC0、单色、24 × 24 视框中的单一路径。
2. 保存为 `internal/gui/assets/logos/<slug>.svg`（小写品牌名，无分隔符）。只能包含单色图形：不含文字、嵌入图片或滤镜（`ui.ParseSVG`）；颜色由所在行决定。
3. 在 `SOURCES.md` 中添加一行（文件、品牌、来源链接、许可证、商标所有者）。
4. 在 `internal/gui/vendorlogo.go` 的 `vendorKeywords` 中加入该品牌的主机名词语，并在 `TestVendorLogoName` 中添加用例。文件无法解析、或关键词对应的标志缺失时，`TestVendorLogosParse` 会失败。

商标所有者要求时，相应标志会被移除。
