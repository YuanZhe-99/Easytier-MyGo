#!/bin/bash
# Builds the release artifacts with `mygo build`.
#
#   scripts/build.sh                  this machine's platform
#   scripts/build.sh linux/amd64 windows/amd64 ...
#
# For each target it fetches the pinned EasyTier core (verified, UPX
# removed) and builds easytier-mygo-svc and easytier-mygo-ctl into
# resources/<goos>-<goarch>/bin, which `mygo build` ships with the app.
# Linux .deb packages get their dependencies corrected afterwards: MyGo
# declares WebKitGTK for every app, which a native-UI app does not use.
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
done

go tool mygo build -platform "$(IFS=,; echo "${targets[*]}")"

# Correct the Debian dependencies: GTK 3 and the tray library, no WebKitGTK.
for deb in build/*/*.deb; do
	[ -e "$deb" ] || continue
	work=$(mktemp -d)
	dpkg-deb -R "$deb" "$work"
	sed -i -E '/^Depends:/{s/,? *libwebkit2gtk-4\.1-0[^,]*//; s/^Depends: *, */Depends: /}' "$work/DEBIAN/control"
	grep -q '^Depends:.*libayatana-appindicator3-1' "$work/DEBIAN/control" ||
		sed -i -E 's/^Depends: (.*)$/Depends: \1, libayatana-appindicator3-1/' "$work/DEBIAN/control"
	dpkg-deb --root-owner-group -b "$work" "$deb" >/dev/null
	rm -rf "$work"
	echo "fixed dependencies of $deb: $(dpkg-deb -f "$deb" Depends)"
done
