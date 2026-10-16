# 翻译指南

## 1. 范围

`doc/en-us/` 是权威版本，`doc/zh-cn/` 镜像它；界面字符串在 ARB 目录中（英文、简体中文、繁体中文、日文）。

## 2. 结构一致

两个目录包含相同的文件，标题数、表格行数和代码块数量都相同；代码块逐字节一致。`go run ./tools/checkdocs` 负责检查。

## 3. 不翻译的内容

代码、命令、路径、配置键、标识符、版本号、产品名（EasyTier、MyGo、Tailscale）以及健康术语的取值（`local-fault`、`degraded`……）。

## 4. 风格

句子简短直白。中文使用全角标点，中文与拉丁文字之间仅在有助阅读时加空格。

## 5. 术语表

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
| health | 健康状态 |
| bounded recovery | 有限恢复 |
| save / apply | 保存 / 应用 |
| tested / untested | 已测试 / 未测试 |
| instance ID | 实例 ID |
| Dock (macOS) | 程序坞 |

## 6. 审阅清单

- 两个目录的文件、标题、表格和代码块一致。
- 术语使用一致。
- 链接在各自目录中指向相同的文档。
