package main

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/commands"
)

func TestChannelSlashCommandsExposedByBrokerRegistry(t *testing.T) {
	r := commands.NewRegistry()
	commands.RegisterAllCommands(r)

	for _, cmd := range channelSlashCommands {
		if _, ok := r.Get(cmd.Name); !ok {
			t.Errorf("channel slash command %q missing from broker registry", cmd.Name)
		}
	}
}
