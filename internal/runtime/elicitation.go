package runtime

import (
	"encoding/gob"
)

type ElicitationInterruptInfo struct {
	Kind            string
	ActionID        string
	Message         string
	RequestedSchema any
}

type ElicitationInterruptState struct {
	ActionID string
}

func init() {
	gob.Register(ElicitationInterruptState{})
}
