# 健康监测

服务显示“运行中”时，内核可能已经卡死，或者内核进程还活着但网络实例已退出（EasyTier 的 `--daemon` 守护会让进程在没有运行实例时继续存活；已在 v2.6.4 验证）。只看进程无法发现这些情况。Daemon 按组件判定健康、记录证据，只恢复那些能明确归因于本机的故障。

## 模型

| 术语 | 取值 | 含义 |
|---|---|---|
| Desired | `run`、`stop` | 用户意图。停止优先于一切，恢复绝不违背它。 |
| Lifecycle | `starting`、`running`、`stopping`、`stopped`、`unknown` | Daemon 观察到的进程状态。 |
| Level | `unknown`、`healthy`、`degraded`、`failed` | 经确认阈值后的判定。 |
| Cause | `none`、`local-fault`、`unresponsive`、`external`、`collector`、`awaiting` | 归因。只有 `local-fault` 与 `unresponsive` 可以触发恢复。 |
| Recovery | `idle`、`scheduled`、`running`、`exhausted` | 恢复流程所处阶段。 |

## 每次检查观察什么

1. **内核进程**——进程已退出为 `failed`/`local-fault`。
2. **内核响应**（`easytier-cli node`，有超时上限）：
   - 超时 → `failed`/`unresponsive`；
   - 连接被拒绝或 “no running instances found” → `failed`/`local-fault`；
   - 缺少二进制、输出无法解析 → `unknown`/`collector`：不归咎于内核，也不重启任何东西。
3. **网络实例**——运行且有地址 → `healthy`；尚无地址或没有节点 → `degraded`/`awaiting`。安静的单节点网络绝不会导致重启。

只有能明确归因于本机的观察才会推进失败计数。

## 策略

`internal/health.Policy` 是这些数字的唯一出处（产品决策，不是上游默认值）：

| 参数 | 默认值 | 作用 |
|---|---|---|
| FailThreshold | 3 | 连续多少次可归因失败后判为 `failed`。 |
| OKThreshold | 2 | 连续多少次健康观察后清除故障。 |
| GracePeriod | 45 s | 主动启动/应用/唤醒后，失败仍会显示但不安排恢复。 |
| BackoffBase / Max | 5 s / 60 s | 每次重试延迟翻倍，有上限，±20 % 抖动。 |
| MaxAttempts / Window | 3 / 10 min | 每组件的滑动预算，跨恢复轮次、跨 daemon 重启累计。 |
| RecoveryEnabled | true | 只监控模式会关闭所有自动恢复。 |

## 恢复规则

1. 判为 `failed` 的组件一次只安排一个动作，在退避延迟后执行。
2. 动作开始即消耗预算；之后的观察决定它是否有效——“启动命令返回了”不算成功。
3. 重启后组件进入宽限期，启动过程中的瞬态不会再被惩罚。
4. 预算按时间窗口计算而非按故障轮次，并且会持久化，所以崩溃循环或 daemon 重启都不能无限重启。手动启动会重新授权恢复；手动停止绝不会被覆盖。
5. 预算耗尽时停止自动恢复并记录事件；监测继续。

## 事件

级别变化、恢复的安排/开始/结束以及预算耗尽都会成为事件，包含时间、组件、级别、归因、原因、脱敏后的详情和尝试次数。事件会持久化，之后打开的 GUI 也能看到它关闭期间发生了什么。

## 呈现

GUI 使用面向用户的状态说明。内部原因码、策略数字和 revision hash 只出现在诊断包和日志详情里，不出现在主界面。陈旧或已断开的数据总会被标出；绿色状态绝不会比证据活得更久。
