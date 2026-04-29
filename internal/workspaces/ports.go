package workspaces

import "errors"

const (
	// PortRangeStart is the first even broker port for non-main workspaces.
	PortRangeStart = 7910
	// PortRangeEnd is the last valid broker port (web port = broker+1 ≤ 7999).
	PortRangeEnd = 7998

	// MainBrokerPort / MainWebPort are the legacy defaults for the "main"
	// workspace (preserved from the pre-multi-workspace single-broker default).
	MainBrokerPort = 7890
	MainWebPort    = 7891
)

// ErrPortPoolExhausted is returned when all port pairs in the range are
// already claimed by registered workspaces.
var ErrPortPoolExhausted = errors.New("workspaces: port pool exhausted (range 7910–7999 full)")

// AllocatePortPair scans the registry for the next free even-port pair
// (broker on N, web on N+1) starting at PortRangeStart. Port allocation
// trusts the registry — no lsof scan. Bind conflicts surface at broker
// startup and suggest `wuphf workspace doctor`.
//
// reg may be nil or have an empty Workspaces slice for the first workspace.
func AllocatePortPair(reg *Registry) (broker int, web int, err error) {
	used := make(map[int]bool)
	if reg != nil {
		for _, ws := range reg.Workspaces {
			used[ws.BrokerPort] = true
			used[ws.WebPort] = true
		}
	}

	for bp := PortRangeStart; bp <= PortRangeEnd; bp += 2 {
		wp := bp + 1
		if !used[bp] && !used[wp] {
			return bp, wp, nil
		}
	}
	return 0, 0, ErrPortPoolExhausted
}
