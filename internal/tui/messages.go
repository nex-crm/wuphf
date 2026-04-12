package tui

// PickerSelectMsg is emitted when a picker option is selected.
type PickerSelectMsg struct {
	Value string
	Label string
}

// ConfirmMsg is emitted by a picker confirm dialog.
type ConfirmMsg struct {
	Confirmed bool
}

// InitFlowMsg signals a phase transition in the init flow.
type InitFlowMsg struct {
	Phase string
	Data  map[string]string
}
