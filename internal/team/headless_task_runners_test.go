package team

import (
	"reflect"
	"testing"
)

func TestParseHeadlessTaskRunnerProcesses(t *testing.T) {
	input := []byte(" 123 node /opt/homebrew/bin/codex -a never -s workspace-write exec -C /private/var/folders/x/T/wuphf-task-task-3 -c mcp_servers.wuphf-office.command=\"/tmp/wuphf\" -\n456 /usr/bin/other\n")
	got := parseHeadlessTaskRunnerProcesses(input)
	want := []headlessTaskRunnerProcess{
		{
			PID:     123,
			Command: "node /opt/homebrew/bin/codex -a never -s workspace-write exec -C /private/var/folders/x/T/wuphf-task-task-3 -c mcp_servers.wuphf-office.command=\"/tmp/wuphf\" -",
		},
		{
			PID:     456,
			Command: "/usr/bin/other",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseHeadlessTaskRunnerProcesses() = %#v, want %#v", got, want)
	}
}

func TestKillStaleHeadlessTaskRunnersKillsOnlyMatchingProcesses(t *testing.T) {
	setListHeadlessTaskRunnerProcessesForTest(t, func() ([]byte, error) {
		return []byte("123 node /opt/homebrew/bin/codex -a never -s workspace-write exec -C /private/var/folders/x/T/wuphf-task-task-3 -c mcp_servers.wuphf-office.command=\"/tmp/wuphf\" -\n124 /opt/homebrew/bin/codex exec -C /tmp/elsewhere -\n125 node /opt/homebrew/bin/codex -a never -s workspace-write exec -C /private/var/folders/x/T/wuphf-task-task-9 -c something_else=1 -\n"), nil
	})

	var killed []int
	setKillHeadlessTaskRunnerProcessForTest(t, func(pid int) {
		killed = append(killed, pid)
	})

	killStaleHeadlessTaskRunners()

	if !reflect.DeepEqual(killed, []int{123}) {
		t.Fatalf("killStaleHeadlessTaskRunners() killed %#v, want [123]", killed)
	}
}
