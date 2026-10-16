# Development

## Requirements

- Go 1.27.2 (`go.mod`); MyGo is a Go tool dependency (`go tool mygo …`).
- Linux desktop for the GUI; `libayatana-appindicator3-1` for the tray.
- `upx` to unpack the bundled EasyTier binaries (see [easytier-upstream.md](easytier-upstream.md)).

## Commands

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

## Dev loop

Run the daemon against a private socket, state directory and explicit ports, then point the GUI at
it:

```bash
go build -o build/easytier-mygo-svc ./cmd/easytier-mygo-svc
go build -o build/easytier-mygo-ctl ./cmd/easytier-mygo-ctl
cp resources/linux-amd64/bin/easytier-* build/
build/easytier-mygo-svc run --host user --rpc-base 25888 \
  --socket "$PWD/.dev/session/ctl.sock" --state-dir "$PWD/.dev/session/state" &
C="build/easytier-mygo-ctl -socket $PWD/.dev/session/ctl.sock"
$C create "Dev" .dev/dev-network.toml
$C connect <ID>
$C watch <ID>      # networks run while a session is attached
```

The dev network config needs explicit, unused listener ports (`listeners = ["tcp://0.0.0.0:21110"]`)
and `[flags] no_tun = true` unless the privileged path is being tested. A second local core must
dial the machine's interface address, not `127.0.0.1`.

## Live adapter test

The gated test runs only when `EASYTIER_CLI` (path to `easytier-cli`) and `EASYTIER_RPC` (an
isolated dev core's portal) are set. Re-capture `internal/coreadapter/testdata` from a real core
when the bundled EasyTier version moves.

## Testing principles

Tests protect product contracts and error-prone logic, preferably table-driven; every fixed bug
gets a regression test. No tests for wording, internal state details or evidence collection.
`go test ./...` stays under a minute.
