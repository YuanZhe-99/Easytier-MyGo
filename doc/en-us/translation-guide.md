# Translation guide

## 1. Scope

`doc/en-us/` is authoritative. `doc/zh-cn/` mirrors it; interface strings live in the ARB
catalogs (English, Simplified Chinese, Traditional Chinese, Japanese).

## 2. Structural parity

Both trees contain the same files with the same number of headings, table rows and code blocks;
code blocks are byte-identical. `go run ./tools/checkdocs` enforces this.

## 3. Never translated

Code, commands, paths, config keys, identifiers, version numbers, product names (EasyTier, MyGo,
Tailscale) and the health vocabulary values (`local-fault`, `degraded`, …).

## 4. Style

Plain, short sentences. Chinese text uses full-width punctuation, with a space between Chinese
and Latin words only where it helps readability.

## 5. Glossary

| English | 简体中文 |
|---|---|
| run mode | 运行方式 |
| No privileges | 免授权 |
| Follow GUI | 随界面 |
| Background service | 后台服务 |
| privileged helper | 特权助手 |
| operator | 操作者（operator） |
| core | 内核 |
| core manager | 内核管理 |
| network (profile) | 网络 |
| peer | 节点 |
| pane (the floating box beside the sidebar) | 小窗 |
| sidebar / rail | 侧边栏 / 图标栏 |
| Settings sheet | 设置面板 |
| notice card | 提示卡片 |
| virtual device | 虚拟网卡 |
| traffic lights (macOS) | 红绿灯按钮 |
| health | 健康状态 |
| bounded recovery | 有限恢复 |
| save / apply | 保存 / 应用 |
| tested / untested | 已测试 / 未测试 |
| instance ID | 实例 ID |
| Dock (macOS) | 程序坞 |
| MagicDNS name | MagicDNS 名称 |
| vendor logo | 厂商标志 |
| friendly name (of a peer) | 友好名称 |
| code signature | 代码签名 |
| designated requirement (macOS) | 指定要求 |
| notarization / notarized | 公证 / 经过公证 |
| quarantine (macOS) | 隔离属性 |

## 6. Review checklist

- Same files, headings, tables and code blocks in both trees.
- Glossary terms used consistently.
- Links point to the same documents in each tree.
