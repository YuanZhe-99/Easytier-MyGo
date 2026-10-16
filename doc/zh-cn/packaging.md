# 打包

`go tool mygo build` 生成各平台产物：Linux 上是 `.tar.gz`（附 `install.sh`，按用户安装）和 `.deb`；Windows 上是 `.exe` 与 NSIS 安装包；macOS 上是 `.app` 与 `.dmg`（需要 macOS 主机）。

## 附带的二进制

`resources/<goos>-<goarch>/` 下的文件随应用一起分发，运行时通过 `mygo.App.Path(mygo.PathResources)` 找到。`go run ./tools/deps fetch -platform <goos>-<goarch>` 下载固定的 EasyTier 压缩包、校验 SHA-256，并把内核与 CLI（去掉 UPX）解压到 `resources/<goos>-<goarch>/bin`。这些目录是构建输入，不提交到仓库。

## 许可证

EasyTier MyGo 采用 GPL-3.0。内置的 EasyTier 二进制为 LGPL-3.0：每个分发包都附带其许可证文本和 `third_party/easytier.json` 中记录的源码地址。临时图标来自 EasyTier-iOS（GPL-3.0-or-later）；见 `resources/NOTICE`。
