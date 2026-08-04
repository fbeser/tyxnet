package commands

import (
	"context"
	"testing"
)

func TestRejectsArbitraryCommand(t *testing.T) {
	if err := ExecuteSystem(context.Background(), "echo unsafe"); err == nil {
		t.Fatal("arbitrary command accepted")
	}
}
