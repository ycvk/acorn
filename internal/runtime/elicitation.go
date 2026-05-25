package runtime

type ElicitationInterruptInfo struct {
	Kind            string
	ActionID        string
	Message         string
	RequestedSchema any
}

type ElicitationInterruptState struct {
	ActionID string
}
