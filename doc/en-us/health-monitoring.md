# Health monitoring

A service can be "running" while the core is hung, or while the core process is alive but its
network instance has exited (EasyTier's `--daemon` guard keeps the process alive with no running
instances; verified with v2.6.4). Process supervision alone cannot see that. The daemon judges
health per component, records evidence, and recovers only what is attributably broken on this
machine.

## Model

| Word | Values | Meaning |
|---|---|---|
| Desired | `run`, `stop` | The user's intent. A stop wins over everything; recovery never runs against it. |
| Lifecycle | `starting`, `running`, `stopping`, `stopped`, `unknown` | What the process is doing, as observed by the daemon. |
| Level | `unknown`, `healthy`, `degraded`, `failed` | The settled verdict after confirmation thresholds. |
| Cause | `none`, `local-fault`, `unresponsive`, `external`, `collector`, `awaiting` | Who is at fault. Only `local-fault` and `unresponsive` may trigger recovery. |
| Recovery | `idle`, `scheduled`, `running`, `exhausted` | Where the recovery pipeline stands. |

## What a check observes

1. **Core process** — a dead process is `failed`/`local-fault`.
2. **Core response** (`easytier-cli node`, bounded by a timeout):
   - timeout → `failed`/`unresponsive`;
   - connection refused or "no running instances found" → `failed`/`local-fault`;
   - missing binary, unparsable output → `unknown`/`collector`: nothing is blamed on the core and
     nothing restarts.
3. **Network instance** — running with an address → `healthy`; no address yet or zero peers →
   `degraded`/`awaiting`. A quiet single-node network never causes a restart.

Failure counters move only on attributably local observations.

## Policy

`internal/health.Policy` is the single place for the numbers (product decisions, not upstream
defaults):

| Parameter | Default | Effect |
|---|---|---|
| FailThreshold | 3 | Consecutive attributable failures before `failed`. |
| OKThreshold | 2 | Consecutive healthy observations before a fault clears. |
| GracePeriod | 45 s | After a deliberate start/apply/wake, failures show but no recovery is scheduled. |
| BackoffBase / Max | 5 s / 60 s | Retry delay doubles per attempt, capped, ±20 % jitter. |
| MaxAttempts / Window | 3 / 10 min | Sliding budget per component, counted across recovery episodes and daemon restarts. |
| RecoveryEnabled | true | Monitor-only operation turns all automatic recovery off. |

## Recovery rules

1. A `failed` component schedules one action at a time; it runs after its backoff delay.
2. Starting an action consumes budget; the next observations decide whether it helped — "the start
   command returned" is not success.
3. After a restart the component gets grace, so startup transients are not punished.
4. The budget is per window, not per episode, and it is persisted, so a crash loop or a daemon
   restart cannot restart forever. A manual start re-authorizes recovery; a manual stop is never
   overridden.
5. Exhaustion stops automatic recovery and records an event; monitoring continues.

## Events

Level changes, recovery schedule/start/finish and exhaustion are events with time, component,
levels, cause, reason, scrubbed detail and attempt number. They are persisted, so a GUI opened
later sees what happened while it was closed.

## Presentation

The GUI shows plain-language states. Internal reason codes, policy numbers and revision hashes
belong in diagnostics and the log details, not in the main interface. Stale or disconnected data is
always marked; green never outlives its evidence.
