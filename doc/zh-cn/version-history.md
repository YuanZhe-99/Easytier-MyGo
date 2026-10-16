# 版本历史

项目于 2026-10-16 从 0.1.0 重新开始。此前继承的历史（直到 0.10.80）保存在 git tag `archive/v0.10.80` 下，不再延续。

## 0.1.0 — 重构（未发布）

- 重置基线：删除继承的测试、测量脚本、验收文档和 ACL 编辑引擎；重写文档。
- 标识改为 `com.yuanzhe.easytiermygo`，模块改为 `github.com/YuanZhe-99/Easytier-MyGo`。
- MyGo v0.5.5 / EasyTier v2.6.4（2026-10-16 检查）。
- 获取 EasyTier 发布二进制时解除 UPX 压缩（每次 CLI 调用的 CPU 从约 250 ms 降到 3 ms）。
