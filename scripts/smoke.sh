#!/bin/bash
# End-to-end smoke test of the daemon and the privileged helper on this
# machine, with a no_tun network on explicit ports (25888/21110). Meant for
# CI runners, which have administrator rights:
#
#   scripts/smoke.sh user     the no-privileges daemon
#   scripts/smoke.sh helper   install the helper, run the network, uninstall
#
# Never run it on a machine whose EasyTier uses the same ports.
set -euo pipefail
cd "$(dirname "$0")/.."
mode=${1:-user}
goos=$(go env GOOS)
key="$goos-$(go env GOARCH)"
ext=""
[ "$goos" = windows ] && ext=".exe"
bin="$PWD/resources/$key/bin"

go run ./tools/deps fetch -platform "$key"
for cmd in easytier-mygo-svc easytier-mygo-ctl; do
	go build -o "$bin/$cmd$ext" "./cmd/$cmd"
done
svc="$bin/easytier-mygo-svc$ext"
ctl="$bin/easytier-mygo-ctl$ext"

work=$(mktemp -d)
cat > "$work/net.toml" <<'TOML'
instance_name = "smoke"
hostname = "smoke"
ipv4 = "10.250.9.1"
dhcp = false
listeners = ["tcp://0.0.0.0:21110"]

[network_identity]
network_name = "etm-smoke"
network_secret = "smoke-secret"

[flags]
no_tun = true
TOML

sudo_if_unix() {
	if [ "$goos" = windows ]; then "$@"; else sudo "$@"; fi
}

case "$mode" in
user)
	sock="$work/ctl.sock"
	"$svc" run --host user --socket "$sock" --state-dir "$work/state" --rpc-base 25888 &
	daemon=$!
	sleep 2
	c=("$ctl" -socket "$sock")
	;;
helper)
	if [ "$goos" = windows ]; then
		sid=$(powershell -NoProfile -Command '[System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value' | tr -d '\r')
		"$svc" helper install --from "$bin" --operator-sid "$sid"
	else
		sudo "$svc" helper install --from "$bin" --operator "$(id -u)"
	fi
	sleep 3
	"$svc" helper status
	c=("$ctl")
	;;
*)
	echo "usage: $0 user|helper" >&2
	exit 2
	;;
esac

cleanup() {
	[ -n "${watcher:-}" ] && kill "$watcher" 2>/dev/null || true
	if [ "$mode" = user ]; then
		kill "${daemon:-}" 2>/dev/null || true
	else
		sudo_if_unix "$svc" helper uninstall --purge || true
	fi
}
trap cleanup EXIT

"${c[@]}" create Smoke "$work/net.toml"
id=$("${c[@]}" list | awk '/Smoke/{print $1}')
"${c[@]}" connect "$id"
"${c[@]}" watch "$id" > "$work/watch.log" 2>&1 &
watcher=$!

# A single node has no peers: "connecting" means the core runs and its
# management portal answers, which is what this test checks.
for _ in $(seq 1 60); do
	state=$("${c[@]}" list | awk '/Smoke/{print $3}')
	case "$state" in
	connecting | connected)
		echo "smoke ($mode): network is $state"
		"${c[@]}" list
		exit 0
		;;
	esac
	sleep 1
done
echo "smoke ($mode): network never came up" >&2
"${c[@]}" list >&2 || true
"${c[@]}" events >&2 || true
exit 1
