package agent

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	RegisterTypes()
	os.Exit(m.Run())
}
