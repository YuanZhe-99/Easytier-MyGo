# 开发

## 要求

- Go 1.27.2（`go.mod`）；MyGo 是 Go 工具依赖（`go tool mygo …`）。
- 运行 GUI 需要 Linux 桌面；托盘需要 `libayatana-appindicator3-1`。
- 用 `upx` 解压内置的 EasyTier 二进制（见 [easytier-upstream.md](easytier-upstream.md)）。

## 命令

```bash
go run ./tools/deps check          # pinned vs latest MyGo and EasyTier
go run ./tools/deps fetch          # bundled core into resources/<goos>-<goarch>/bin
gofmt -l cmd internal tools third_party
go vet ./...
go test ./...
CGO_ENABLED=0 go test ./...
go tool mygo vet .
go run ./tools/checkdocs           # doc/en-us and doc/zh-cn mirror each other
```

## 开发循环

让 daemon 使用私有 socket、状态目录和显式端口运行，再让 GUI 指向它：

```bash
B=resources/linux-amd64/bin
go build -o build/easytier-mygo-svc ./cmd/easytier-mygo-svc
build/easytier-mygo-svc \
  --core-bin $B/easytier-core --cli-bin $B/easytier-cli \
  --config "$PWD/.dev/dev-network.toml" --rpc 127.0.0.1:25888 \
  --socket "$PWD/.dev/session/ctl.sock" --state-dir "$PWD/.dev/session/state" \
  --config-dir "$PWD/.dev/session/networks"
EASYTIER_MYGO_SOCKET="$PWD/.dev/session/ctl.sock" go tool mygo dev
```

开发用网络配置需要显式且未占用的监听端口（`listeners = ["tcp://0.0.0.0:21110"]`），除非在测试特权路径，否则要设 `[flags] no_tun = true`。第二个本地内核必须拨本机网卡地址，而不是 `127.0.0.1`。

## Live adapter 测试

只有设置了 `EASYTIER_CLI`（`easytier-cli` 路径）和 `EASYTIER_RPC`（隔离的开发内核的 portal）时，这个门控测试才会运行。内置 EasyTier 版本变化时，要从真实内核重新采集 `internal/coreadapter/testdata`。

## 测试原则

测试保护产品契约和容易出错的逻辑，优先使用表驱动；每个修复的 bug 都要有回归测试。不为文案、内部状态细节或收集证据写测试。`go test ./...` 保持在一分钟以内。
