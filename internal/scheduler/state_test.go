package scheduler

import (
	"github.com/waris4ly/packets/pkg/apitypes"
	"testing"
)

func TestStateMachine(t *testing.T) {
	sm := NewStateMachine()

	err := sm.ValidateTransition(apitypes.JobStatePending, apitypes.JobStateDispatched)
	if err != nil {
		t.Errorf("expected transition to succeed, got %v", err)
	}

	err = sm.ValidateTransition(apitypes.JobStatePending, apitypes.JobStateSucceeded)
	if err == nil {
		t.Error("expected transition to fail, got nil")
	}

	err = sm.ValidateTransition(apitypes.JobStateDispatched, apitypes.JobStateFallbackLocal)
	if err != nil {
		t.Errorf("expected transition to succeed, got %v", err)
	}
}
