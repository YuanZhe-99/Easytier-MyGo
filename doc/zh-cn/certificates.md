# 证书

EasyTier 的 MagicDNS 给每台电脑一个名称（`laptop.et.net`），但不提供 HTTPS 证书。daemon内置一个私有证书颁发机构（`internal/certmanager`）：为网络的 MagicDNS 名称、主机名和虚拟 IP 签发 TLS 服务器证书，写到 Web 服务器（Caddy、Nginx、Docker 容器）读取的位置，在到期前续期，并在之后运行一条命令。需要接受这些证书的设备只需信任一次该 CA。

## 证书颁发机构

- **生成**：默认 ECDSA P-256（可选 RSA 2048 或 4096），默认有效期 10 年（1 到 30 年）。该证书是只能签发叶子证书的 CA（`IsCA`，路径长度 0），可签发证书和 CRL，并带有 SubjectKeyId。
- **导入**：PEM 证书及其私钥（PKCS #8、PKCS #1 或 SEC 1，未加密）。只有私钥属于该证书、证书是允许签发证书的 CA 且当前有效时才会导入，否则拒绝。
- **替换**：已有 CA 时再生成或导入需要明确选择替换；旧文件保留为 `ca.crt.old` / `ca.key.old`，所有任务都会重新签发。
- **导出**：导出 CA 证书（安装到手机和其他电脑），或作为单独选项导出证书和私钥，以便在另一台电脑上使用同一个 CA。私钥以 0600 权限写入，命令行会提示：持有私钥的人可以签发受信任的证书。

文件位于 `<state>/certs/`：`ca.crt`（0644）、`ca.key`（0600）、`jobs.json`、`state.json`（历史记录、最近的 Hook 结果），以及默认输出目录 `out/<job>/`。

## 信任库

把 CA 安装进操作系统的信任库会改变这台电脑上所有程序信任的内容，因此daemon从不替客户端执行这一操作。`easytier-mygo-ctl cert trust install|uninstall|status` 在调用者自己的进程中运行：已提权时调用平台工具；否则不做任何改动，并打印一条可在管理员终端中运行的命令。daemon的状态会报告 CA 是否已受信任，并给出同样的命令。

| 系统 | 识别方式 | 安装 | 是否已安装 |
|---|---|---|---|
| Windows | — | `certutil -addstore -f Root <ca.crt>` | `certutil -store Root <SHA-1>` |
| macOS | — | `security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain <ca.crt>` | System 钥匙串的 `security find-certificate -a -Z` 中含有其 SHA-1 |
| Debian、Ubuntu、Alpine | `/etc/os-release`，否则 PATH 中有 `update-ca-certificates` | 复制到 `/usr/local/share/ca-certificates/<name>.crt`，运行 `update-ca-certificates` | 系统证书包中含有该 CA |
| RHEL、CentOS、Fedora | `/etc/os-release`，否则 PATH 中有 `update-ca-trust` | 复制到 `/etc/pki/ca-trust/source/anchors/<name>.crt`，运行 `update-ca-trust extract` | 同上 |
| openSUSE | `/etc/os-release` | 复制到 `/etc/pki/trust/anchors/<name>.crt`，运行 `update-ca-certificates` | 同上 |
| Arch | `/etc/os-release`，否则 PATH 中有 `trust` | `trust anchor --store <ca.crt>` | 同上 |

`<name>` 是 `easytier-mygo-ca-` 加上 CA 的 SHA-256 的前 8 位十六进制数，因此替换后的 CA 不会覆盖旧 CA 的文件。即使使用 `sudo`，macOS 也会弹出对话框要求确认。Firefox 使用自己的信任库，除非开启了其企业根证书设置。

## 任务

一个任务就是一张保持最新的证书（`certmanager.Job`，保存在 `jobs.json`）：

| 字段 | 默认值 | 含义 |
|---|---|---|
| `network` | — | 网络 ID，证书包含其运行中内核报告的名称：`<hostname>`、`<hostname>.<zone>`（开启 MagicDNS 时）和虚拟 IPv4 |
| `dns_names`、`ips` | — | 额外的名称；DNS 名称可以以一个通配符标签开头（`*.et.net`） |
| `out_dir` | `<state>/certs/out/<id>` | 绝对路径目录，不存在时自动创建（0755） |
| `cert_file`、`fullchain_file`、`key_file` | `cert.pem`、`fullchain.pem`、`key.pem` | 纯文件名；`-` 表示不写证书或完整证书链 |
| `cert_mode`、`key_mode` | `0644`、`0600` | 八进制权限（Unix）；例如为容器的用户组设为 `0640` |
| `validity_days` | 90 | 最多 397，即 Apple 平台和 Chrome 接受的最长期限 |
| `renew_before_days` | 30 | 续期窗口；有效期的三分之一也算，取两者中较长者 |
| `key_type` | `ecdsa-p256` | 或 `rsa-2048`、`rsa-4096` |
| `hook` | 关闭 | 新证书写入后运行的命令，见下文 |

证书把所有名称写入 SAN 扩展（`DNSNames`、`IPAddresses`）；通用名称只重复第一个名称。扩展密钥用法为服务器身份验证。叶子证书的有效期不会超过其 CA。`fullchain.pem` 是叶子证书后接 CA 证书。

网络名称只来自运行中的内核。网络停止期间沿用最后一次看到的名称，因此停止网络不会让证书少掉名称。如果任务的网络从未运行过，任务会等待（状态 `waiting`），而不会签发缺少这些名称的证书；强制续期则只用配置的名称签发。

## 续期

daemon启动时检查所有任务，之后每 6 小时检查一次；网络报告的名称变化后一分钟内也会检查。修改任务或 CA 的请求会立即检查，`cert.renew` 按需检查（`force` 表示无论如何都签发）。满足以下条件时签发新证书：

| 原因 | 条件 |
|---|---|
| `missing` | 证书、完整证书链或私钥文件不存在 |
| `corrupt` | 文件无法解析、私钥不匹配，或两个证书文件不一致 |
| `ca-changed` | 证书无法通过当前 CA 的验证 |
| `san-changed` | 需要的名称与证书中的不同 |
| `expiring` | 剩余时间少于续期窗口，或已过期 |
| `forced` | 用户要求 |

否则不写任何文件，也不运行 Hook（`none`）。新文件先在目标目录中写成临时文件并同步到磁盘，全部完成后才依次重命名覆盖旧文件，私钥最后；写入失败时旧文件保持不变。续期只在daemon运行时进行：随界面方式下在应用打开期间，后台服务方式下从开机起。

## Hook

Hook 只在写入新证书之后运行：Linux 和 macOS 上用 `sh -c`，Windows 上用 `cmd /C`，或 `powershell -NoProfile -NonInteractive -Command`，也可以是不经过 shell 的参数列表。`{{CERT_PATH}}`、`{{KEY_PATH}}`、`{{FULLCHAIN_PATH}}` 和 `{{OUT_DIR}}` 会被替换，同样的值也在 `EASYTIER_MYGO_CERT_PATH`、`…_KEY_PATH`、`…_FULLCHAIN_PATH` 和 `…_OUT_DIR` 中。Hook 在输出目录中以daemon的权限运行（在特权助手中以 root/SYSTEM 身份，且须经批准），最长运行其超时时间（默认 30 秒，最多 600 秒）；超时后在 Unix 上结束其整个进程组，在 Windows 上结束其进程树。输出的最后 16 KiB、退出码，以及失败是否像是缺少权限（退出码 126、“permission denied”、Docker 套接字等）都会记录在任务状态和daemon日志中。Hook 失败不会撤销新证书：任务会显示 Hook 警告。

## 特权助手

特权助手以 root/SYSTEM 身份运行，接受操作者（operator）和 `easytier-mygo` 用户组的写入请求。为了不让用户组成员借 root 写入任意文件或运行命令，特权助手中的任务只有在管理员恰好批准了这些设置时，才会写入自选文件夹（`out_dir`）或运行命令；否则写入 `<state>/certs/out/<id>/`，保存时失败并返回 `needs-admin`。

- **批准**：以管理员权限运行 `easytier-mygo-svc helper approve-cert -file JOB.json`（或用 `-job` 传入 base64url 编码的任务 JSON）；没有管理员权限时它拒绝运行。它把任务的 ID、文件夹（及其解析后的位置，跟随符号链接）、文件名和权限以及命令记录到 `<state>/certs/approved.json`（`certmanager.Approve`）。任何本地 API 请求都不能添加批准；删除任务会同时删除它的批准。
- **两次检查**：保存任务时检查一次，每次写入或运行命令前再检查一次。文件夹、文件名、权限或命令发生变化，批准在此后被替换，或文件夹现在解析到别处，都会让任务停止（失败，不写入也不运行），直到再次批准。名称和有效期可以随意修改，无需批准。
- **命令以 root/SYSTEM 身份运行。** 请选择只有管理员才能写入的文件夹。
- **界面**：在“设置 › 证书”中保存这样的任务时会请求管理员授权（pkexec、macOS 密码、UAC），然后保存。**命令行**：`cert job-set` 把任务写入一个文件并打印 `approve-cert` 命令，之后用 `cert job-set -file` 保存。

用户daemon（免授权方式）不需要批准；它的 Hook 以该用户身份运行。

## 本地 API 与命令行

所有 `cert.*` 请求都属于写权限级别：它们带有路径、Hook 输出，导出时还带有 CA 私钥。

| 请求 | 参数（`Request.Cert`） | 响应 |
|---|---|---|
| `cert.status` | — | `Response.Cert`（`certmanager.Status`） |
| `cert.ca.generate` | `ca`、`replace` | 检查所有任务后的状态 |
| `cert.ca.import` | `cert_pem`、`key_pem`、`replace` | 同上 |
| `cert.ca.export` | `with_key` | `Response.CertExport` |
| `cert.job.set` | `job`（按 ID 替换） | 检查该任务后的状态 |
| `cert.job.remove` | `job_id` | 状态；任务的文件保留 |
| `cert.renew` | `job_id`（为空：所有任务）、`force` | 状态 |

界面在“设置 › 证书”中提供同样的功能（[gui.md](gui.md)）。`easytier-mygo-ctl cert` 封装了这些请求：

```bash
easytier-mygo-ctl cert ca-generate -cn "Home CA"
easytier-mygo-ctl cert trust install          # elevated, or prints the command to run
easytier-mygo-ctl cert job-set -id web -network NETWORK-ID -dns '*.laptop.et.net' \
  -out "$HOME/certs/web" -hook 'caddy reload --config /etc/caddy/Caddyfile'
easytier-mygo-ctl cert status
easytier-mygo-ctl cert renew -force web
easytier-mygo-ctl cert ca-export home-ca.crt  # for phones and other computers
```
