# Broker Spawn

The desktop shell supervises a broker utility process. The current broker entry
is `src/main/broker-stub-entry.ts`, which uses Electron's `process.parentPort`
utility-process wire to send liveness pings until the loopback listener branch
replaces it.

```mermaid
sequenceDiagram
  participant App as app.whenReady
  participant Main as Electron main
  participant Supervisor as BrokerSupervisor
  participant Utility as utilityProcess.fork
  participant Broker as wuphf-broker

  App->>Main: createSecureWindow()
  Main->>Supervisor: start()
  Supervisor->>Utility: fork(broker-stub-entry.js, serviceName: wuphf-broker)
  Utility->>Broker: boot utility process
  Broker-->>Supervisor: parentPort.postMessage({ alive: true }) every 1s
  Main->>Supervisor: app.before-quit stop()
  Supervisor->>Broker: postMessage({ type: "shutdown" })
  alt POSIX
    Supervisor->>Broker: UtilityProcess.kill() handle-bound cleanup
  else Windows
    Supervisor->>Broker: taskkill /pid <pid> /T
  end
  Supervisor->>Supervisor: wait 5s grace
  Supervisor->>Broker: repeat handle-bound kill, or taskkill /F on Windows
```

## Crash-Restart Policy

Unexpected broker exits are restarted with exponential backoff:

```text
backoffMs = min(60_000, firstBackoffMs * 2 ** (restartCount - 1))
```

`restartCount` increments before each scheduled retry, so the first wait is
250ms, then 500ms, 1000ms, and so on. After five consecutive retries the
supervisor enters a fatal state, reports the failure to the main process, and
does not restart again. If a restarted broker remains alive past the 60s
stability window, the retry counter resets to zero. Status reported through IPC
remains lifecycle-only: `starting`, `alive`, `unresponsive`, `dead`, or
`unknown`; `unresponsive` means the process has not sent a liveness ping within
5s.

The supervisor reads monotonic time through `src/main/monotonic-clock.ts` for
restart metadata, the stability window, and liveness staleness. Wall-clock Date
APIs remain banned.

## Env Allowlist

The broker does not inherit the full parent environment. Only these variables
are passed through:

| Variable | Why |
|---|---|
| `PATH` | Allows the utility process to resolve normal local tooling when needed. |
| `HOME` | Keeps OS-level path resolution consistent without exposing app data. |
| `USER` | Standard OS identity metadata for local process behavior. |
| `LANG` | Locale for deterministic text behavior. |
| `LC_ALL` | Locale override when explicitly set by the user environment. |
| `TZ` | Time zone context for future user-facing local formatting. |

Secrets, tokens, cloud credentials, and app-data paths are not passed through.
