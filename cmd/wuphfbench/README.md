# wuphfbench

`wuphfbench` is the desktop SLO probe driver for the WUPHF broker and web
proxy startup path.

SLOs are advisory; CI gates flip from advisory to required only after macOS/Windows baselines exist.

## Build

```bash
go build -o wuphf ./cmd/wuphf
go build -o wuphfbench ./cmd/wuphfbench
```

The benchmark binary looks for `./wuphf` first, then `wuphf` on `PATH`.
Set `WUPHF_BENCH_WUPHF_BINARY=/path/to/wuphf` to benchmark a specific build.

## Usage

```bash
./wuphfbench --probe cold-start
./wuphfbench --probe cold-start --iters 3
./wuphfbench --probe ipc-latency
./wuphfbench --probe all --out markdown
./scripts/bench-desktop.sh
```

The wrapper writes JSON to `bench/results/<timestamp>.json`. Result files are
local artifacts and should not be committed.

## Probes

`cold-start` launches `wuphf --no-open` on fresh loopback ports for each
iteration, polls the web proxy at `/api/health`, records launch-to-HTTP-200,
and tears the process down before the next iteration. The default is 20
iterations.

`ipc-latency` launches one broker/web proxy pair, waits for `/api/health`, then
issues sequential `GET /api/health` requests through a `net/http` client. The
default is 1000 iterations.

Both implemented probes report milliseconds with p50, p95, p99, and raw
samples. JSON for a single probe is:

```json
{"probe":"cold-start","iters":3,"unit":"ms","p50":123.456,"p95":150.789,"p99":150.789,"raw":[120.001,123.456,150.789]}
```

TODO stubs are present for RSS sampling, SSE latency, render perf, and
install-size probes. RSS will need platform-specific collection: `gopsutil` or
`/proc` parsing on Linux, `ps` on macOS, and `tasklist` on Windows.
