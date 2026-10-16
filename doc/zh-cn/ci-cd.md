# CI/CD

工作流位于 `.github/workflows`，在 GitHub 上运行。私有 Gitea 远端没有 runner，推送到那里只经过本地检查。

| 工作流 | 触发 | 内容 |
|---|---|---|
| `ci.yml` | 推送到 `main`、拉取请求、手动 | 格式、vet、测试（含 `CGO_ENABLED=0`）、MyGo vet、文档镜像检查、上游固定版本；在 Windows 与 macOS 上构建并跑单元测试。 |
| `upstream.yml` | 每周、手动 | `go run ./tools/deps check -strict`；MyGo 或 EasyTier 有新版时开 issue。 |
| `dependabot.yml` | 每周 | Action 与 Go 模块更新。 |

发布工作流（tag `v*`）随打包一起加入；见 [packaging.md](packaging.md)。

## 上游升级流程

每个变更集都先运行 `go run ./tools/deps check`。MyGo 或 EasyTier 有新版本时，在一个提交里一起改：

1. `go run ./tools/deps update`（MyGo 升到最新；EasyTier 清单改为最新稳定版）。
2. 运行 `go tool mygo migrate-ui .`，再加 `-write`，并人工复核标识/绑定值相关的改动。
3. `go run ./tools/deps fetch`，重新采集 `internal/coreadapter/testdata`，运行 live adapter 测试和完整的本地检查。
4. 只有 live 检查通过后，才把新的 EasyTier tag 加入 `third_party/easytier.json` 的 `tested`；更新 [README.md](README.md) 中的版本表。
5. 在 [version-history.md](version-history.md) 记录“MyGo vX / EasyTier vY（YYYY-MM-DD 检查）”，没有变化也要记录。

内置内核只跟随稳定版（GitHub 的 “latest”）。预发布版通过应用内的内核管理试用，从不随发布物分发。
