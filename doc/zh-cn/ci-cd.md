# CI/CD

工作流位于 `.github/workflows`，在 GitHub 上运行。私有 Gitea 远端没有 runner，推送到那里只经过本地检查。

| 工作流 | 触发 | 内容 |
|---|---|---|
| `ci.yml` | 推送到 `main`、拉取请求、手动 | 格式、vet、测试（含 `CGO_ENABLED=0`）、MyGo vet、界面目录、文档镜像检查、上游固定版本；在 Windows（x64 与 ARM64）与 macOS 上构建并跑单元测试。 |
| `ci.yml` → smoke | 检查之后 | 在 Linux、macOS、Windows（x64、ARM64）runner 上运行 `scripts/smoke.sh user` 和 `helper`：分别经用户态 daemon 和已安装的助手运行一个 `no_tun` 网络，然后卸载。 |
| `upstream.yml` | 每周、手动 | `go run ./tools/deps check -strict`；MyGo 或 EasyTier 有新版时开 issue。 |
| `dependabot.yml` | 每周 | Action 与 Go 模块更新。 |

`release.yml`（tag `v*` 或手动触发）按平台运行 `scripts/build.sh`：Linux 与 Windows 在 Ubuntu 上构建（安装包用 NSIS），macOS 在 macOS runner 上构建。推送 tag 时先确认它与 `internal/version` 一致，再连同 `SHA256SUMS.txt` 发布下载。密钥 `EASYTIER_MYGO_SIGNING_KEY`（由 `go run ./tools/sign keygen` 生成）用于给构建包签名；私钥只保存在 secret 和维护者的离线副本中。密钥 `MYGO_UPDATER_PRIVATE_KEY`（由 `go tool mygo keygen` 生成；公钥是 `mygo.json` 中的 `updates.publicKey`）用于给应用更新签名：每个平台一个压缩包、相对最近几个版本的增量包和 `update-<target>.json`，都附在 release 上。已安装的应用读取最新 release 的清单；没有这个 secret 就不发布更新，已安装的应用停留在原版本。密钥 `EASYTIER_MYGO_MACOS_CERT_P12`（Apple Development 证书及其私钥，base64 编码的 `.p12`）和 `EASYTIER_MYGO_MACOS_CERT_PASSWORD`（其密码）用于给 macOS 应用签名：任务把它们导入临时钥匙串，仅当证书是自签名时才把它信任为根证书，并把 `MYGO_SIGN` 设为证书的 SHA-1（[packaging.md](packaging.md)）；没有它们时应用以 ad hoc 方式签名。随后用 `hdiutil verify` 检查每个磁盘映像，并用 `codesign --verify --deep --strict` 检查其中的应用。

## 上游升级流程

每个变更集都先运行 `go run ./tools/deps check`。MyGo 或 EasyTier 有新版本时，在一个提交里一起改：

1. `go run ./tools/deps update`（MyGo 升到最新；EasyTier 清单改为最新稳定版）。
2. 运行 `go tool mygo migrate-ui .`，再加 `-write`，并人工复核标识/绑定值相关的改动。
3. `go run ./tools/deps fetch`，重新采集 `internal/coreadapter/testdata`，运行 live adapter 测试和完整的本地检查。
4. 只有 live 检查通过后，才把新的 EasyTier tag 加入 `third_party/easytier.json` 的 `tested`；更新 [README.md](README.md) 中的版本表。
5. 在 [version-history.md](version-history.md) 记录“MyGo vX / EasyTier vY（YYYY-MM-DD 检查）”，没有变化也要记录。

内置内核只跟随稳定版（GitHub 的 “latest”）。预发布版通过应用内的内核管理试用，从不随发布物分发。
