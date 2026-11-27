# Certificates

EasyTier's MagicDNS gives every computer a name (`laptop.et.net`), but no certificate for HTTPS.
The daemon runs a private certificate authority (`internal/certmanager`): it issues TLS server
certificates for a network's MagicDNS name, hostname and virtual IP, writes them where a web server
(Caddy, Nginx, a Docker container) reads them, renews them before they expire, and runs a command
afterwards. Devices that should accept these certificates trust the CA once.

## Certificate authority

- **Generate**: ECDSA P-256 by default (RSA 2048 or 4096 on request), 10 years by default (1 to
  30). The certificate is a CA limited to issuing leaves (`IsCA`, path length 0), may sign
  certificates and CRLs, and carries a SubjectKeyId.
- **Import**: a PEM certificate and its private key (PKCS #8, PKCS #1 or SEC 1, unencrypted). The
  import is refused unless the key belongs to the certificate, the certificate is a CA allowed to
  sign certificates, and it is valid now.
- **Replace**: generating or importing when a CA exists needs an explicit replace; the old files
  stay as `ca.crt.old` / `ca.key.old` and every job is re-issued.
- **Export**: the CA certificate (to install on phones and other computers), or, as a separate
  option, the certificate with its private key to run the same CA on another computer. The key is
  written with mode 0600 and the CLI warns that anyone holding it can issue trusted certificates.

Files live in `<state>/certs/`: `ca.crt` (0644), `ca.key` (0600), `jobs.json`, `state.json`
(history, last hook results) and the default output directories `out/<job>/`.

## Trust store

Installing the CA into the operating system's trust store changes what every program on the
computer trusts, so the daemon never does it on a client's behalf. `easytier-mygo-ctl cert trust
install|uninstall|status` runs in the caller's own process: elevated, it runs the platform's tool;
otherwise it changes nothing and prints the command to run in an administrator shell. The daemon's
status reports whether the CA is trusted and the same commands.

| System | Detected by | Install | Installed check |
|---|---|---|---|
| Windows | — | `certutil -addstore -f Root <ca.crt>` | `certutil -store Root <SHA-1>` |
| macOS | — | `security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain <ca.crt>` | SHA-1 in `security find-certificate -a -Z` of the System keychain |
| Debian, Ubuntu, Alpine | `/etc/os-release`, else `update-ca-certificates` on PATH | copy to `/usr/local/share/ca-certificates/<name>.crt`, `update-ca-certificates` | the system bundle contains the CA |
| RHEL, CentOS, Fedora | `/etc/os-release`, else `update-ca-trust` on PATH | copy to `/etc/pki/ca-trust/source/anchors/<name>.crt`, `update-ca-trust extract` | same |
| openSUSE | `/etc/os-release` | copy to `/etc/pki/trust/anchors/<name>.crt`, `update-ca-certificates` | same |
| Arch | `/etc/os-release`, else `trust` on PATH | `trust anchor --store <ca.crt>` | same |

`<name>` is `easytier-mygo-ca-` plus the first 8 hex digits of the CA's SHA-256, so a replaced CA
never overwrites the old one's file. macOS asks for confirmation in a dialog even under `sudo`.
Firefox keeps its own store unless its enterprise roots setting is on.

## Jobs

A job is one certificate kept current (`certmanager.Job`, stored in `jobs.json`):

| Field | Default | Meaning |
|---|---|---|
| `network` | — | Network ID whose names the certificate carries: `<hostname>`, `<hostname>.<zone>` (when MagicDNS is on) and the virtual IPv4, as its running core reports them |
| `dns_names`, `ips` | — | Extra names; DNS names may start with one wildcard label (`*.et.net`) |
| `out_dir` | `<state>/certs/out/<id>` | Absolute directory, created (0755) when missing |
| `cert_file`, `fullchain_file`, `key_file` | `cert.pem`, `fullchain.pem`, `key.pem` | Plain file names; `-` leaves the certificate or the full chain out |
| `cert_mode`, `key_mode` | `0644`, `0600` | Octal permissions (Unix); e.g. `0640` for a container's group |
| `validity_days` | 90 | At most 397, the longest Apple platforms and Chrome accept |
| `renew_before_days` | 30 | Renewal window; a third of the lifetime counts too, whichever is longer |
| `key_type` | `ecdsa-p256` | Or `rsa-2048`, `rsa-4096` |
| `hook` | off | Command after a new certificate, see below |

Certificates carry every name in the SAN extension (`DNSNames`, `IPAddresses`); the common name
only repeats the first one. Extended key usage is server authentication. A leaf never outlives its
CA. `fullchain.pem` is the leaf followed by the CA.

Network names come only from a running core. While the network is stopped the last names seen are
kept, so stopping a network never shrinks a certificate. A job whose network was never seen
running waits (phase `waiting`) instead of issuing a certificate without those names; a forced
renewal issues with the configured names alone.

## Renewal

The manager checks every job when the daemon starts, every 6 hours after that, and within a minute
of a network reporting other names. Requests that change a job or the CA check at once, and
`cert.renew` checks on demand (`force` issues regardless). A check issues a new certificate when:

| Reason | When |
|---|---|
| `missing` | the certificate, full chain or key file is absent |
| `corrupt` | a file does not parse, the key does not match, or the two certificate files differ |
| `ca-changed` | the certificate does not verify against the current CA |
| `san-changed` | the wanted names differ from the certificate's |
| `expiring` | less than the renewal window remains, or it has expired |
| `forced` | the user asked |

Otherwise nothing is written and the hook does not run (`none`). New files are written as
temporary files in the target directory, synced, and renamed over the old ones only when all of
them are complete, key last; a failed write leaves the old files. Renewal runs only while the
daemon runs: in Follow GUI mode while the app is open, in Background service mode from boot.

## Hooks

A hook runs only after a new certificate was written: `sh -c` on Linux and macOS, `cmd /C` on
Windows, or `powershell -NoProfile -NonInteractive -Command`, or an argument list without a shell.
`{{CERT_PATH}}`, `{{KEY_PATH}}`, `{{FULLCHAIN_PATH}}` and `{{OUT_DIR}}` are replaced, and the same
values are in `EASYTIER_MYGO_CERT_PATH`, `…_KEY_PATH`, `…_FULLCHAIN_PATH` and `…_OUT_DIR`. The hook
runs in the output directory with the daemon's rights (as root/SYSTEM in the privileged helper,
only when approved), for at most its timeout (30 s by default,
600 s at most); on Unix its whole process group is killed then, on Windows its process tree. The
last 16 KiB of its output, its exit code and whether the failure looks like missing rights
(exit code 126, "permission denied", the Docker socket, …) are kept in the job's status and the
daemon log. A failed hook never undoes the new certificate: the job shows a hook warning.

## Privileged helper

The helper runs as root/SYSTEM and accepts writes from the operator and the `easytier-mygo`
group. So that a group member cannot make root write arbitrary files or run commands, a job in the
helper writes to a folder of its own (`out_dir`) or runs a command only while an administrator
has approved exactly those settings; otherwise it writes to `<state>/certs/out/<id>/` and saving it
fails with `needs-admin`.

- **Approving** is `easytier-mygo-svc helper approve-cert -file JOB.json` (or `-job` with the job
  as base64url JSON), run with administrator rights; it refuses to run without them. It records
  the job's ID, the folder (and where it resolves, symbolic links followed), the file names and
  permissions and the command in `<state>/certs/approved.json` (`certmanager.Approve`). No local
  API request adds an approval; removing a job drops its approval.
- **Checked twice:** when the job is saved and again before every write or command run. A
  changed folder, file name, permission or command, an approval replaced since, or a folder that
  now resolves elsewhere stops the job (it fails, nothing is written or run) until it is approved
  again. Names and the lifetime may change without approval.
- **The command runs as root/SYSTEM.** Choose a folder only administrators can write to.
- **GUI:** saving such a job in Settings › Certificates asks for administrator approval (pkexec,
  the macOS password, UAC), then saves it. **CLI:** `cert job-set` writes the job to a file and
  prints the `approve-cert` command, then `cert job-set -file` saves it.

The user daemon (No privileges mode) needs no approval; its hooks run as the user.

## Local API and CLI

All `cert.*` requests are write tier: they carry paths, hook output and, on export, the CA key.

| Request | Arguments (`Request.Cert`) | Answer |
|---|---|---|
| `cert.status` | — | `Response.Cert` (`certmanager.Status`) |
| `cert.ca.generate` | `ca`, `replace` | status, after checking every job |
| `cert.ca.import` | `cert_pem`, `key_pem`, `replace` | same |
| `cert.ca.export` | `with_key` | `Response.CertExport` |
| `cert.job.set` | `job` (replaced by ID) | status, after checking that job |
| `cert.job.remove` | `job_id` | status; the job's files stay |
| `cert.renew` | `job_id` (empty: every job), `force` | status |

The GUI offers the same in Settings › Certificates ([gui.md](gui.md)). `easytier-mygo-ctl cert`
wraps them:

```bash
easytier-mygo-ctl cert ca-generate -cn "Home CA"
easytier-mygo-ctl cert trust install          # elevated, or prints the command to run
easytier-mygo-ctl cert job-set -id web -network NETWORK-ID -dns '*.laptop.et.net' \
  -out "$HOME/certs/web" -hook 'caddy reload --config /etc/caddy/Caddyfile'
easytier-mygo-ctl cert status
easytier-mygo-ctl cert renew -force web
easytier-mygo-ctl cert ca-export home-ca.crt  # for phones and other computers
```
