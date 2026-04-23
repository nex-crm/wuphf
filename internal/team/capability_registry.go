package team

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/config"
)

type CapabilityLifecycle string

const (
	CapabilityLifecycleReady        CapabilityLifecycle = "ready"
	CapabilityLifecycleNeedsSetup   CapabilityLifecycle = "needs_setup"
	CapabilityLifecycleDisabled     CapabilityLifecycle = "disabled"
	CapabilityLifecycleDeferred     CapabilityLifecycle = "deferred"
	CapabilityLifecyclePartial      CapabilityLifecycle = "partial"
	CapabilityLifecycleProvisioning CapabilityLifecycle = "provisioning"
)

type CapabilityCategory string

const (
	CapabilityCategoryRuntime  CapabilityCategory = "runtime"
	CapabilityCategoryMemory   CapabilityCategory = "memory"
	CapabilityCategoryAction   CapabilityCategory = "action"
	CapabilityCategoryWorkflow CapabilityCategory = "workflow"
	CapabilityCategoryOffice   CapabilityCategory = "office"
	CapabilityCategoryDirect   CapabilityCategory = "direct"
)

const (
	CapabilityKeyTmux          = "tmux"
	CapabilityKeyClaude        = "claude"
	CapabilityKeyCodex         = "codex"
	CapabilityKeyOpencode      = "opencode"
	CapabilityKeyOfficeRuntime = "office_runtime"
	CapabilityKeyDirectRuntime = "direct_runtime"
	CapabilityKeyMemory        = "memory"
	CapabilityKeyNex           = CapabilityKeyMemory
	CapabilityKeyConnections   = "connections"
	CapabilityKeyActions       = "actions"
	CapabilityKeyWorkflows     = "workflows"
	CapabilityKeyOfficeActions = "office_actions"
	CapabilityKeyDirectActions = "direct_actions"
)

type CapabilityProbeOptions struct {
	IncludeConnections bool
	ConnectionLimit    int
	ConnectionTimeout  time.Duration
}

type CapabilityDescriptor struct {
	Key       string
	Label     string
	Category  CapabilityCategory
	Level     CapabilityLevel
	Lifecycle CapabilityLifecycle
	Detail    string
	NextStep  string
}

type CapabilityRegistry struct {
	Entries []CapabilityDescriptor
}

var actionProvidersFn = func() []action.Provider {
	return []action.Provider{
		action.NewComposioFromEnv(),
		action.NewOneCLIFromEnv(),
	}
}

var actionProviderForCapabilityFn = func(cap action.Capability) (action.Provider, error) {
	return action.NewRegistryFromEnv().ProviderFor(cap)
}

var actionConnectionsProbeFn = func(ctx context.Context, provider action.Provider, opts action.ListConnectionsOptions) (action.ConnectionsResult, error) {
	return provider.ListConnections(ctx, opts)
}

func (r CapabilityRegistry) Entry(key string) (CapabilityDescriptor, bool) {
	key = strings.TrimSpace(key)
	for _, entry := range r.Entries {
		if strings.EqualFold(strings.TrimSpace(entry.Key), key) {
			return entry, true
		}
	}
	return CapabilityDescriptor{}, false
}

func (r CapabilityRegistry) SummaryStatuses(keys ...string) []CapabilityStatus {
	if len(keys) == 0 {
		keys = make([]string, 0, len(r.Entries))
		for _, entry := range r.Entries {
			keys = append(keys, entry.Key)
		}
	}
	statuses := make([]CapabilityStatus, 0, len(keys))
	for _, key := range keys {
		entry, ok := r.Entry(key)
		if !ok {
			continue
		}
		statuses = append(statuses, CapabilityStatus{
			Name:      entry.Label,
			Level:     entry.Level,
			Lifecycle: entry.Lifecycle,
			Detail:    entry.Detail,
			NextStep:  entry.NextStep,
		})
	}
	return statuses
}

func BuildCapabilityRegistry(runtime RuntimeCapabilities) CapabilityRegistry {
	if len(runtime.Registry.Entries) > 0 {
		return runtime.Registry
	}
	return buildCapabilityRegistry(config.ResolveLLMProvider(""), runtimeCapabilityStatus(runtime, "tmux"), runtimeCapabilityStatus(runtime, "claude"), runtimeCapabilityStatus(runtime, "codex"), runtimeCapabilityStatus(runtime, "opencode"), CapabilityProbeOptions{})
}

func buildCapabilityRegistry(providerName string, tmuxStatus, claudeStatus, codexStatus, opencodeStatus CapabilityStatus, opts CapabilityProbeOptions) CapabilityRegistry {
	entries := []CapabilityDescriptor{
		buildOfficeRuntimeDescriptor(providerName, tmuxStatus, claudeStatus, codexStatus, opencodeStatus),
		buildDirectRuntimeDescriptor(providerName, claudeStatus, codexStatus, opencodeStatus),
		descriptorFromStatus(CapabilityKeyTmux, "tmux", CapabilityCategoryRuntime, tmuxStatus),
		descriptorFromStatus(CapabilityKeyClaude, "claude", CapabilityCategoryRuntime, claudeStatus),
		descriptorFromStatus(CapabilityKeyCodex, "codex", CapabilityCategoryRuntime, codexStatus),
		descriptorFromStatus(CapabilityKeyOpencode, "opencode", CapabilityCategoryRuntime, opencodeStatus),
		buildMemoryDescriptor(),
		buildActionCapabilityDescriptor(CapabilityKeyActions, "Action execution", CapabilityCategoryAction, action.CapabilityActionExecute),
		buildActionCapabilityDescriptor(CapabilityKeyWorkflows, "Workflow execution", CapabilityCategoryWorkflow, action.CapabilityWorkflowExecute),
		buildActionCapabilityDescriptor(CapabilityKeyOfficeActions, "Office actions", CapabilityCategoryOffice, action.CapabilityActionExecute),
		buildActionCapabilityDescriptor(CapabilityKeyDirectActions, "Direct actions", CapabilityCategoryDirect, action.CapabilityActionExecute),
	}
	if opts.IncludeConnections {
		entries = append(entries, buildConnectionsDescriptor(opts))
	}
	return CapabilityRegistry{Entries: entries}
}

func ResolveActionProviderForCapability(cap action.Capability) (action.Provider, error) {
	return actionProviderForCapabilityFn(cap)
}

func RegistryKeyForActionCapability(cap action.Capability) string {
	switch cap {
	case action.CapabilityConnections:
		return CapabilityKeyConnections
	case action.CapabilityWorkflowExecute, action.CapabilityWorkflowRuns, action.CapabilityWorkflowCreate:
		return CapabilityKeyWorkflows
	default:
		return CapabilityKeyActions
	}
}

func buildOfficeRuntimeDescriptor(providerName string, tmuxStatus, claudeStatus, codexStatus, opencodeStatus CapabilityStatus) CapabilityDescriptor {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "codex":
		level := codexStatus.Level
		lifecycle := CapabilityLifecycleReady
		if level != CapabilityReady {
			lifecycle = CapabilityLifecycleNeedsSetup
		}
		return CapabilityDescriptor{
			Key:       CapabilityKeyOfficeRuntime,
			Label:     "Office runtime",
			Category:  CapabilityCategoryOffice,
			Level:     level,
			Lifecycle: lifecycle,
			Detail:    codexStatus.Detail,
			NextStep:  codexStatus.NextStep,
		}
	case "opencode":
		level := opencodeStatus.Level
		lifecycle := CapabilityLifecycleReady
		if level != CapabilityReady {
			lifecycle = CapabilityLifecycleNeedsSetup
		}
		return CapabilityDescriptor{
			Key:       CapabilityKeyOfficeRuntime,
			Label:     "Office runtime",
			Category:  CapabilityCategoryOffice,
			Level:     level,
			Lifecycle: lifecycle,
			Detail:    opencodeStatus.Detail,
			NextStep:  opencodeStatus.NextStep,
		}
	}
	level := CapabilityReady
	lifecycle := CapabilityLifecycleReady
	nextStep := ""
	if tmuxStatus.Level != CapabilityReady || claudeStatus.Level != CapabilityReady {
		level = CapabilityWarn
		lifecycle = CapabilityLifecycleNeedsSetup
		nextStep = firstNonEmpty(tmuxStatus.NextStep, claudeStatus.NextStep)
	}
	return CapabilityDescriptor{
		Key:       CapabilityKeyOfficeRuntime,
		Label:     "Office runtime",
		Category:  CapabilityCategoryOffice,
		Level:     level,
		Lifecycle: lifecycle,
		Detail:    strings.TrimSpace(strings.Join(compactStrings([]string{tmuxStatus.Detail, claudeStatus.Detail}), " ")),
		NextStep:  nextStep,
	}
}

func buildDirectRuntimeDescriptor(providerName string, claudeStatus, codexStatus, opencodeStatus CapabilityStatus) CapabilityDescriptor {
	status := claudeStatus
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "codex":
		status = codexStatus
	case "opencode":
		status = opencodeStatus
	}
	level := status.Level
	lifecycle := CapabilityLifecycleReady
	if level != CapabilityReady {
		lifecycle = CapabilityLifecycleNeedsSetup
	}
	return CapabilityDescriptor{
		Key:       CapabilityKeyDirectRuntime,
		Label:     "Direct runtime",
		Category:  CapabilityCategoryDirect,
		Level:     level,
		Lifecycle: lifecycle,
		Detail:    status.Detail,
		NextStep:  status.NextStep,
	}
}

func buildMemoryDescriptor() CapabilityDescriptor {
	status := ResolveMemoryBackendStatus()
	switch status.SelectedKind {
	case config.MemoryBackendNone:
		return CapabilityDescriptor{
			Key:       CapabilityKeyMemory,
			Label:     "Memory backend",
			Category:  CapabilityCategoryMemory,
			Level:     CapabilityInfo,
			Lifecycle: CapabilityLifecycleDisabled,
			Detail:    status.Detail,
			NextStep:  status.NextStep,
		}
	default:
		if status.ActiveKind == config.MemoryBackendNone {
			label := status.SelectedLabel + " memory"
			if strings.TrimSpace(label) == "" {
				label = "Memory backend"
			}
			return CapabilityDescriptor{
				Key:       CapabilityKeyMemory,
				Label:     label,
				Category:  CapabilityCategoryMemory,
				Level:     CapabilityWarn,
				Lifecycle: CapabilityLifecycleNeedsSetup,
				Detail:    status.Detail,
				NextStep:  status.NextStep,
			}
		}
		label := status.ActiveLabel + " memory"
		return CapabilityDescriptor{
			Key:       CapabilityKeyMemory,
			Label:     label,
			Category:  CapabilityCategoryMemory,
			Level:     CapabilityReady,
			Lifecycle: CapabilityLifecycleReady,
			Detail:    status.Detail,
		}
	}
}

func buildActionCapabilityDescriptor(key, label string, category CapabilityCategory, cap action.Capability) CapabilityDescriptor {
	if config.ResolveNoNex() {
		return CapabilityDescriptor{
			Key:       key,
			Label:     label,
			Category:  category,
			Level:     CapabilityInfo,
			Lifecycle: CapabilityLifecycleDisabled,
			Detail:    "Disabled for this session with --no-nex.",
			NextStep:  "Restart without --no-nex to enable provider-backed actions.",
		}
	}
	provider, err := ResolveActionProviderForCapability(cap)
	if err != nil {
		return CapabilityDescriptor{
			Key:       key,
			Label:     label,
			Category:  category,
			Level:     CapabilityWarn,
			Lifecycle: CapabilityLifecycleNeedsSetup,
			Detail:    err.Error(),
			NextStep:  "Configure a supported action provider or connect the required account.",
		}
	}
	return CapabilityDescriptor{
		Key:       key,
		Label:     label,
		Category:  category,
		Level:     CapabilityReady,
		Lifecycle: CapabilityLifecycleReady,
		Detail:    fmt.Sprintf("%s is available via %s.", label, provider.Name()),
	}
}

func buildConnectionsDescriptor(opts CapabilityProbeOptions) CapabilityDescriptor {
	if config.ResolveNoNex() {
		return CapabilityDescriptor{
			Key:       CapabilityKeyConnections,
			Label:     "Connected accounts",
			Category:  CapabilityCategoryAction,
			Level:     CapabilityInfo,
			Lifecycle: CapabilityLifecycleDisabled,
			Detail:    "Disabled for this session with --no-nex.",
			NextStep:  "Restart without --no-nex to enable live connected accounts.",
		}
	}
	provider, err := ResolveActionProviderForCapability(action.CapabilityConnections)
	if err != nil {
		return CapabilityDescriptor{
			Key:       CapabilityKeyConnections,
			Label:     "Connected accounts",
			Category:  CapabilityCategoryAction,
			Level:     CapabilityWarn,
			Lifecycle: CapabilityLifecycleNeedsSetup,
			Detail:    err.Error(),
			NextStep:  "Configure an action provider and connect an account.",
		}
	}

	timeout := opts.ConnectionTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	limit := opts.ConnectionLimit
	if limit <= 0 {
		limit = 5
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := actionConnectionsProbeFn(ctx, provider, action.ListConnectionsOptions{Limit: limit})
	if err != nil {
		return CapabilityDescriptor{
			Key:       CapabilityKeyConnections,
			Label:     "Connected accounts",
			Category:  CapabilityCategoryAction,
			Level:     CapabilityWarn,
			Lifecycle: CapabilityLifecycleProvisioning,
			Detail:    err.Error(),
			NextStep:  "Reconnect the action provider and rerun /doctor.",
		}
	}
	if len(result.Connections) == 0 {
		return CapabilityDescriptor{
			Key:       CapabilityKeyConnections,
			Label:     "Connected accounts",
			Category:  CapabilityCategoryAction,
			Level:     CapabilityWarn,
			Lifecycle: CapabilityLifecycleNeedsSetup,
			Detail:    fmt.Sprintf("%s is configured, but no connected accounts are available.", provider.Name()),
			NextStep:  "Connect Gmail, CRM, or another provider-backed account.",
		}
	}
	return CapabilityDescriptor{
		Key:       CapabilityKeyConnections,
		Label:     "Connected accounts",
		Category:  CapabilityCategoryAction,
		Level:     CapabilityReady,
		Lifecycle: CapabilityLifecycleReady,
		Detail:    fmt.Sprintf("%d account%s ready through %s.", len(result.Connections), capabilityPluralSuffix(len(result.Connections)), provider.Name()),
	}
}

func capabilityKey(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, " ", "_")
	return name
}

func descriptorFromStatus(key, label string, category CapabilityCategory, status CapabilityStatus) CapabilityDescriptor {
	lifecycle := status.Lifecycle
	if lifecycle == "" {
		switch status.Level {
		case CapabilityReady:
			lifecycle = CapabilityLifecycleReady
		case CapabilityWarn:
			lifecycle = CapabilityLifecycleNeedsSetup
		default:
			lifecycle = CapabilityLifecycleProvisioning
		}
	}
	return CapabilityDescriptor{
		Key:       key,
		Label:     label,
		Category:  category,
		Level:     status.Level,
		Lifecycle: lifecycle,
		Detail:    status.Detail,
		NextStep:  status.NextStep,
	}
}

func runtimeCapabilityStatus(runtime RuntimeCapabilities, name string) CapabilityStatus {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "tmux" && runtime.Tmux.hasData() {
		status := runtime.Tmux.status()
		if strings.TrimSpace(status.Name) == "" {
			status.Name = "tmux"
		}
		return status
	}
	if name == "codex" && strings.TrimSpace(runtime.Codex.Name) != "" {
		status := runtime.Codex
		if strings.TrimSpace(status.Name) == "" {
			status.Name = "codex"
		}
		return status
	}
	if name == "opencode" && strings.TrimSpace(runtime.Opencode.Name) != "" {
		status := runtime.Opencode
		if strings.TrimSpace(status.Name) == "" {
			status.Name = "opencode"
		}
		return status
	}
	for _, item := range runtime.Items {
		if strings.EqualFold(strings.TrimSpace(item.Name), name) {
			return item
		}
	}
	return CapabilityStatus{Name: name, Level: CapabilityInfo}
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func capabilityPluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
