package scheduler

import (
	"errors"
	"fmt"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

var ErrInvalidStateTransition = errors.New("invalid state transition")

type StateMachine struct{}

func NewStateMachine() *StateMachine {
	return &StateMachine{}
}

func (s *StateMachine) ValidateTransition(from, to apitypes.JobState) error {
	switch from {
	case apitypes.JobStatePending:
		if to == apitypes.JobStateUploading || to == apitypes.JobStateDispatched || to == apitypes.JobStateFailed || to == apitypes.JobStateFallbackLocal {
			return nil
		}
	case apitypes.JobStateUploading:
		if to == apitypes.JobStateDispatched || to == apitypes.JobStateFailed || to == apitypes.JobStateFallbackLocal {
			return nil
		}
	case apitypes.JobStateDispatched:
		if to == apitypes.JobStateRunning || to == apitypes.JobStateFailed || to == apitypes.JobStateFallbackLocal {
			return nil
		}
	case apitypes.JobStateRunning:
		if to == apitypes.JobStateSucceeded || to == apitypes.JobStateFailed || to == apitypes.JobStateFallbackLocal {
			return nil
		}
	case apitypes.JobStateSucceeded, apitypes.JobStateFailed, apitypes.JobStateFallbackLocal:
		return fmt.Errorf("ValidateTransition: cannot transition from terminal state %v: %w", from, ErrInvalidStateTransition)
	}
	return fmt.Errorf("ValidateTransition: %v to %v: %w", from, to, ErrInvalidStateTransition)
}
