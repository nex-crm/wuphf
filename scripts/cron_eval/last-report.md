# Cron-registry evaluation report

- **Self-registration:** 8/8
- **Floors enforced:** 14/14
- **Disabled-skips-run:** pass
- **Persistence:** n/a (run --verify-persistence)

## Scenarios

| id | title | pass | detail |
|---|---|---|---|
| `cron-01-self-registration` | Self-registration on cold boot | PASS | 8/8 system crons registered with system_managed=true and enabled=true |
| `cron-02-patch-happy-path` | PATCH nex-notifications interval_override=30 | PASS | interval_override=30 as set |
| `cron-03-floor-rejection` | Floor rejection (below=400, at=200) | PASS | 14/14 sub-cases pass |
| `cron-04-read-only` | Read-only cron rejects PATCH (one-relay-events) | PASS | rejected all 3 variants |
| `cron-05-disable-reenable` | Disable + re-enable (task_follow_up) | PASS | round-trip clean |
| `cron-06-disabled-skips-run` | Disabled cron skips run (nex-notifications) | PASS | last_run ''->'', log_lines mentioning nex-notifications: 0->0 |
| `cron-07a-persistence-snapshot` | Persistence snapshot (review-expiry) | PASS | snapshot written to /tmp/wuphf-cron-eval-snapshot.json; restart broker, then run --verify-persistence |

## Sub-results

| scenario | sub | pass | detail |
|---|---|---|---|
| `cron-01-self-registration` | `nex-insights` | PASS |  |
| `cron-01-self-registration` | `nex-notifications` | PASS |  |
| `cron-01-self-registration` | `one-relay-events` | PASS |  |
| `cron-01-self-registration` | `request_follow_up` | PASS |  |
| `cron-01-self-registration` | `review-expiry` | PASS |  |
| `cron-01-self-registration` | `task_follow_up` | PASS |  |
| `cron-01-self-registration` | `task_recheck` | PASS |  |
| `cron-01-self-registration` | `task_reminder` | PASS |  |
| `cron-03-floor-rejection` | `nex-insights below_floor (override=29, floor=30)` | PASS |  |
| `cron-03-floor-rejection` | `nex-insights at_floor (override=30)` | PASS |  |
| `cron-03-floor-rejection` | `nex-notifications below_floor (override=4, floor=5)` | PASS |  |
| `cron-03-floor-rejection` | `nex-notifications at_floor (override=5)` | PASS |  |
| `cron-03-floor-rejection` | `request_follow_up below_floor (override=4, floor=5)` | PASS |  |
| `cron-03-floor-rejection` | `request_follow_up at_floor (override=5)` | PASS |  |
| `cron-03-floor-rejection` | `review-expiry below_floor (override=4, floor=5)` | PASS |  |
| `cron-03-floor-rejection` | `review-expiry at_floor (override=5)` | PASS |  |
| `cron-03-floor-rejection` | `task_follow_up below_floor (override=4, floor=5)` | PASS |  |
| `cron-03-floor-rejection` | `task_follow_up at_floor (override=5)` | PASS |  |
| `cron-03-floor-rejection` | `task_recheck below_floor (override=4, floor=5)` | PASS |  |
| `cron-03-floor-rejection` | `task_recheck at_floor (override=5)` | PASS |  |
| `cron-03-floor-rejection` | `task_reminder below_floor (override=4, floor=5)` | PASS |  |
| `cron-03-floor-rejection` | `task_reminder at_floor (override=5)` | PASS |  |
| `cron-04-read-only` | `one-relay-events payload-0 ({'interval_override': 5})` | PASS |  |
| `cron-04-read-only` | `one-relay-events payload-1 ({'enabled': False})` | PASS |  |
| `cron-04-read-only` | `one-relay-events payload-2 ({'interval_override': 10, 'enabled': True})` | PASS |  |
| `cron-04-read-only` | `one-relay-events state-unchanged` | PASS |  |
