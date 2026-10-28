# 配置

## 原文文档

每个网络都是一份 EasyTier TOML 原文。原文是唯一事实：任何地方都不会解析出模型再序列化覆盖用户的文件，因此注释、未知字段和格式都会保留。

- **Revision** 是文本的 SHA-256。保存时要写明编辑所基于的 revision；不一致则以冲突失败，文件保持不变。
- **写入**是原子的，并会先备份之前的内容（每份文档保留十个备份）。
- **允许保存无效草稿。** 激活时会拒绝它们，并先用所选内核的 `--check-config` 预检。内核会不说明原因就拒绝的值，由应用先检查并指出：不是 UUID 的 `instance_id`（包括空值）。

## 表单投影

表单编辑的是文档的投影（`configdoc.Values`）。`configdoc.Changes` 计算用户改动的字段，`configdoc.ApplyValues` 只改写这些字段所在的文本片段；每次写入后都会重新解析结果。表单无法表示的结构只能用原文编辑。

表单会显示 `instance_id`。它为空或缺失时，会填入一个新的 UUID，并在下次保存时写入；一个按钮可以把它换成新的。新建网络时也会生成一个。

ACL 规则在 TOML 编辑器中编辑；结构化 ACL 编辑器会在后续版本回归。

## 内核版本与 EasyTier mini

投影能读取 EasyTier 2.6 与 2.7 的文档：两种格式的 VPN 入口（客户端地址段或具名客户端）、`tcp_stun_servers`、`socket_mark` 和 `prefer_peer_relay`。编辑器会指出网络所用内核无法运行的设置（`configdoc.VersionConflicts`）：2.7 及以后是 2.6 的入口格式，2.7 之前是 2.7 新增的设置。“优先使用已有中继”只在 2.7 内核上、或文档已设置时提供。

**mini 副本。** EasyTier mini 会静默忽略文档中的许多设置。“创建 mini 副本”（`network.mini-copy`）不修改原网络，而是根据其已保存的文档新增一个名为 “<名称> (mini)” 的网络（名称已被占用时为 “(mini 2)” 等），移除 mini 会忽略的设置（`configdoc.StripForMini`）并换上新的 `instance_id`；原网络保留，作为备份。在 mini 副本的编辑器里，提示会列出 mini 仍会忽略的设置，mini 不支持的行也会注明。

## 保存与应用

保存只写入文档。应用是单独的显式操作：用已保存 revision 的冻结副本重启该网络的内核。如果新 revision 在宽限期内失败，daemon 会回到上一个已应用的 revision 并说明原因。

## 密钥

`network_secret` 与安全模式私钥在表单中遮蔽显示，从不写入日志、API 快照或诊断包，也不会持久化到用户文档以外的地方。
