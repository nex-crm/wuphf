package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultColdStartIters = 20
	defaultIPCIters       = 1000
	healthPath            = "/api/health"
	startupTimeout        = 30 * time.Second
	requestTimeout        = 2 * time.Second
)

const helpText = `Usage:
  wuphfbench [--probe cold-start|ipc-latency|all] [--iters N] [--out json|markdown]

Flags:
  --probe string
        Probe to run: cold-start, ipc-latency, or all (default "all")
  --iters int
        Iterations to run. Defaults to 20 for cold-start and 1000 for ipc-latency
  --out string
        Output format: json or markdown (default "json")
  --help
        Show this help text

Environment:
  WUPHF_BENCH_WUPHF_BINARY   Path to the wuphf binary under test (default "./wuphf", then PATH)
`

type benchConfig struct {
	probe string
	iters int
	out   string
}

type benchResult struct {
	Probe string    `json:"probe"`
	Iters int       `json:"iters"`
	Unit  string    `json:"unit"`
	P50   float64   `json:"p50"`
	P95   float64   `json:"p95"`
	P99   float64   `json:"p99"`
	Raw   []float64 `json:"raw"`
}

type wuphfProcess struct {
	cancel      context.CancelFunc
	cmd         *exec.Cmd
	done        chan error
	runtimeHome string
	tokenFile   string
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	results, err := run(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := writeResults(os.Stdout, cfg.out, results); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func parseFlags(args []string) (benchConfig, error) {
	fs := flag.NewFlagSet("wuphfbench", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { _, _ = fmt.Fprint(fs.Output(), helpText) }

	cfg := benchConfig{}
	fs.StringVar(&cfg.probe, "probe", "all", "Probe to run: cold-start, ipc-latency, or all")
	fs.IntVar(&cfg.iters, "iters", 0, "Iterations to run")
	fs.StringVar(&cfg.out, "out", "json", "Output format: json or markdown")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() != 0 {
		return cfg, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	switch cfg.probe {
	case "cold-start", "ipc-latency", "all":
	default:
		return cfg, fmt.Errorf("invalid --probe %q: expected cold-start, ipc-latency, or all", cfg.probe)
	}
	switch cfg.out {
	case "json", "markdown":
	default:
		return cfg, fmt.Errorf("invalid --out %q: expected json or markdown", cfg.out)
	}
	if cfg.iters < 0 {
		return cfg, fmt.Errorf("--iters must be non-negative")
	}
	return cfg, nil
}

func run(cfg benchConfig) ([]benchResult, error) {
	binary, err := resolveWuphfBinary()
	if err != nil {
		return nil, err
	}

	var results []benchResult
	if cfg.probe == "cold-start" || cfg.probe == "all" {
		result, err := runColdStartProbe(binary, itersForProbe("cold-start", cfg.iters))
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if cfg.probe == "ipc-latency" || cfg.probe == "all" {
		result, err := runIPCLatencyProbe(binary, itersForProbe("ipc-latency", cfg.iters))
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func itersForProbe(probe string, requested int) int {
	if requested > 0 {
		return requested
	}
	if probe == "ipc-latency" {
		return defaultIPCIters
	}
	return defaultColdStartIters
}

func runColdStartProbe(binary string, iters int) (benchResult, error) {
	raw := make([]float64, 0, iters)
	for i := 0; i < iters; i++ {
		start := time.Now()
		proc, endpoint, err := startWuphf(binary)
		if err != nil {
			return benchResult{}, err
		}
		readyErr := waitForHealth(context.Background(), endpoint, proc, startupTimeout)
		proc.stop()
		if readyErr != nil {
			return benchResult{}, readyErr
		}
		raw = append(raw, millisSince(start))
	}
	return summarize("cold-start", iters, raw), nil
}

func runIPCLatencyProbe(binary string, iters int) (benchResult, error) {
	proc, endpoint, err := startWuphf(binary)
	if err != nil {
		return benchResult{}, err
	}
	defer proc.stop()

	if err := waitForHealth(context.Background(), endpoint, proc, startupTimeout); err != nil {
		return benchResult{}, err
	}

	client := &http.Client{Timeout: requestTimeout}
	raw := make([]float64, 0, iters)
	for i := 0; i < iters; i++ {
		start := time.Now()
		if err := healthGET(context.Background(), client, endpoint); err != nil {
			return benchResult{}, fmt.Errorf("ipc-latency iteration %d: %w", i+1, err)
		}
		raw = append(raw, millisSince(start))
	}
	return summarize("ipc-latency", iters, raw), nil
}

func startWuphf(binary string) (*wuphfProcess, string, error) {
	webPort, err := freeLoopbackPort()
	if err != nil {
		return nil, "", fmt.Errorf("allocate web port: %w", err)
	}
	brokerPort, err := freeLoopbackPort()
	if err != nil {
		return nil, "", fmt.Errorf("allocate broker port: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runtimeHome, err := os.MkdirTemp("", "wuphfbench-runtime-")
	if err != nil {
		cancel()
		return nil, "", fmt.Errorf("create runtime home: %w", err)
	}
	args := []string{
		"--web-port", strconv.Itoa(webPort),
		"--broker-port", strconv.Itoa(brokerPort),
		"--no-open",
		"--no-nex",
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	tokenFile := filepath.Join(os.TempDir(), fmt.Sprintf("wuphf-bench-token-%d", brokerPort))
	cmd.Env = append(os.Environ(),
		"WUPHF_NO_NEX=1",
		"WUPHF_RUNTIME_HOME="+runtimeHome,
		"WUPHF_BROKER_PORT="+strconv.Itoa(brokerPort),
		"WUPHF_BROKER_TOKEN_FILE="+tokenFile,
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.RemoveAll(runtimeHome)
		return nil, "", fmt.Errorf("start %s: %w", binary, err)
	}
	proc := &wuphfProcess{
		cancel:      cancel,
		cmd:         cmd,
		done:        make(chan error, 1),
		runtimeHome: runtimeHome,
		tokenFile:   tokenFile,
	}
	go func() {
		proc.done <- cmd.Wait()
		close(proc.done)
	}()
	return proc, fmt.Sprintf("http://127.0.0.1:%d%s", webPort, healthPath), nil
}

func waitForHealth(ctx context.Context, endpoint string, proc *wuphfProcess, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{Timeout: requestTimeout}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := healthGET(waitCtx, client, endpoint); err == nil {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for %s: %w", endpoint, waitCtx.Err())
		case err := <-proc.done:
			return fmt.Errorf("wuphf exited before %s became ready: %w", endpoint, err)
		case <-ticker.C:
		}
	}
}

func healthGET(ctx context.Context, client *http.Client, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned %s", endpoint, resp.Status)
	}
	return nil
}

func (p *wuphfProcess) stop() {
	p.cancel()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		<-p.done
	}
	if p.tokenFile != "" {
		_ = os.Remove(p.tokenFile)
	}
	if p.runtimeHome != "" {
		_ = os.RemoveAll(p.runtimeHome)
	}
}

func freeLoopbackPort() (int, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func resolveWuphfBinary() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("WUPHF_BENCH_WUPHF_BINARY")); raw != "" {
		return raw, nil
	}
	name := "wuphf"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	local := filepath.Join(".", name)
	if info, err := os.Stat(local); err == nil && !info.IsDir() {
		abs, err := filepath.Abs(local)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	if path, err := exec.LookPath("wuphf"); err == nil {
		return path, nil
	}
	return "", errors.New("wuphf binary not found; run `go build -o wuphf ./cmd/wuphf` or set WUPHF_BENCH_WUPHF_BINARY")
}

func summarize(probe string, iters int, raw []float64) benchResult {
	return benchResult{
		Probe: probe,
		Iters: iters,
		Unit:  "ms",
		P50:   percentile(raw, 50),
		P95:   percentile(raw, 95),
		P99:   percentile(raw, 99),
		Raw:   raw,
	}
}

func percentile(raw []float64, p float64) float64 {
	if len(raw) == 0 {
		return 0
	}
	sorted := append([]float64(nil), raw...)
	sort.Float64s(sorted)
	idx := int(math.Ceil((p / 100) * float64(len(sorted))))
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return roundMillis(sorted[idx-1])
}

func millisSince(start time.Time) float64 {
	return roundMillis(float64(time.Since(start).Microseconds()) / 1000)
}

func roundMillis(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func writeResults(w io.Writer, format string, results []benchResult) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		if len(results) == 1 {
			return enc.Encode(results[0])
		}
		return enc.Encode(results)
	case "markdown":
		_, err := fmt.Fprintln(w, "SLOs are advisory; CI gates flip from advisory to required only after macOS/Windows baselines exist.")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, "\n| Probe | Iters | Unit | p50 | p95 | p99 |")
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "|---|---:|---|---:|---:|---:|"); err != nil {
			return err
		}
		for _, result := range results {
			if _, err := fmt.Fprintf(w, "| %s | %d | %s | %.3f | %.3f | %.3f |\n", result.Probe, result.Iters, result.Unit, result.P50, result.P95, result.P99); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

func runRSSSamplingProbe() error {
	return errors.New("rss sampling probe not implemented")
}

func runSSELatencyProbe() error {
	return errors.New("sse latency probe not implemented")
}

func runRenderPerfProbe() error {
	return errors.New("render perf probe not implemented")
}

func runInstallSizeProbe() error {
	return errors.New("install-size probe not implemented")
}
