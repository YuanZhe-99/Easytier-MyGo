#!/bin/bash
# Builds the release artifacts with `mygo build`.
#
#   scripts/build.sh                  this machine's platform
#   scripts/build.sh linux/amd64 windows/amd64 ...
#
# For each target it fetches the pinned EasyTier core (verified, UPX
# removed) and builds easytier-mygo-svc and easytier-mygo-ctl into
# resources/<goos>-<goarch>/bin, which `mygo build` ships with the app.
# Linux packages depend on GTK and the tray library only: mygo.json sets
# linux.webview to false, as the app shows no web page.
# MYGO_SIGN passes a signing identity to `mygo build -sign` (macOS; unset,
# the app is signed ad hoc).
set -euo pipefail
cd "$(dirname "$0")/.."

targets=("$@")
if [ ${#targets[@]} -eq 0 ]; then
	targets=("$(go env GOOS)/$(go env GOARCH)")
fi

for t in "${targets[@]}"; do
	goos=${t%/*}
	goarch=${t#*/}
	key="$goos-$goarch"
	go run ./tools/deps fetch -platform "$key"
	ext=""
	[ "$goos" = windows ] && ext=".exe"
	for cmd in easytier-mygo-svc easytier-mygo-ctl; do
		CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags="-s -w" \
			-o "resources/$key/bin/$cmd$ext" "./cmd/$cmd"
	done
	# Release builds sign the bundle, so the privileged helper can update
	# itself from it without an administrator prompt.
	if [ -n "${EASYTIER_MYGO_SIGNING_KEY:-}" ]; then
		go run ./tools/sign sign -dir "resources/$key/bin" \
			-version "$(sed -n 's/^const Version = "\(.*\)"/\1/p' internal/version/version.go)"
	fi
done

sign=()
if [ -n "${MYGO_SIGN:-}" ]; then
	sign=(-sign "$MYGO_SIGN")
fi
go tool mygo build ${sign[@]+"${sign[@]}"} -platform "$(IFS=,; echo "${targets[*]}")"
