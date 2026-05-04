package main

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/commands"
)

func TestChannelSlashCommandsExposedByBrokerRegistry(t *testing.T) {
	r := commands.NewRegistry()
	commands.RegisterAllCommands(r)

	for _, cmd := range channelSlashCommands {
		if strings.Contains(cmd.Name, " ") {
			if _, ok := r.Get(strings.Fields(cmd.Name)[0]); !ok {
				t.Errorf("multi-word channel slash command %q missing parent command", cmd.Name)
			}
			continue
		}
		if _, ok := r.Get(cmd.Name); !ok {
			t.Errorf("channel slash command %q missing from broker registry", cmd.Name)
		}
	}
}
