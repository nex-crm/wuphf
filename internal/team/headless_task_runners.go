package team

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
)

// Test seams behind atomic.Pointer because killStaleHeadlessTaskRunners
// runs at launcher startup, but tests that swap these can outlive
// concurrent launcher constructions in race-suite parallelism.

type listHeadlessTaskRunnerProcessesFn func() ([]byte, error)
type killHeadlessTaskRunnerProcessFn func(pid int)

var (
	listHeadlessTaskRunnerProcessesOverride atomic.Pointer[listHeadlessTaskRunnerProcessesFn]
	killHeadlessTaskRunnerProcessOverride   atomic.Pointer[killHeadlessTaskRunnerProcessFn]
)

func listHeadlessTaskRunnerProcesses() ([]byte, error) {
	if p := listHeadlessTaskRunnerProcessesOverride.Load(); p != nil {
		return (*p)()
	}
	return exec.CommandContext(context.Background(), "ps", "-axo", "pid=,command=").Output()
}

func killHeadlessTaskRunnerProcess(pid int) {
	if p := killHeadlessTaskRunnerProcessOverride.Load(); p != nil {
		(*p)(pid)
		return
	}
	terminateHeadlessProcessPID(pid)
}

type headlessTaskRunnerProcess struct {
	PID     int
	Command string
}

func killStaleHeadlessTaskRunners() {
	output, err := listHeadlessTaskRunnerProcesses()
	if err != nil {
		return
	}
	seen := map[int]struct{}{}
	for _, proc := range parseHeadlessTaskRunnerProcesses(output) {
		if !isHeadlessTaskRunnerCommand(proc.Command) {
			continue
		}
		if _, ok := seen[proc.PID]; ok {
			continue
		}
		seen[proc.PID] = struct{}{}
		killHeadlessTaskRunnerProcess(proc.PID)
	}
}

func parseHeadlessTaskRunnerProcesses(output []byte) []headlessTaskRunnerProcess {
	lines := bytes.Split(output, []byte{'\n'})
	processes := make([]headlessTaskRunnerProcess, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		command := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		if command == "" {
			continue
		}
		processes = append(processes, headlessTaskRunnerProcess{
			PID:     pid,
			Command: command,
		})
	}
	return processes
}

func isHeadlessTaskRunnerCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	return strings.Contains(command, "codex") &&
		strings.Contains(command, "wuphf-task-") &&
		strings.Contains(command, "mcp_servers.wuphf-office.command=")
}
