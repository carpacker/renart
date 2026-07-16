package scheduler

import (
	"errors"
	"fmt"
	"strings"
)

// SchedulerOwnershipState describes whether this process may mutate and run
// schedules. Only the process holding scheduler.lock is an owner; followers
// remain read-only so they cannot persist configuration River will not apply.
type SchedulerOwnershipState string

const (
	SchedulerOwnershipOwner       SchedulerOwnershipState = "owner"
	SchedulerOwnershipFollower    SchedulerOwnershipState = "follower"
	SchedulerOwnershipUnavailable SchedulerOwnershipState = "unavailable"
)

type SchedulerOwnership struct {
	State   SchedulerOwnershipState `json:"state"`
	Message string                  `json:"message,omitempty"`
}

var ErrSchedulerNotOwner = errors.New("scheduler mutations require the lock owner")

func (s *Service) Ownership() SchedulerOwnership {
	if s == nil {
		return SchedulerOwnership{
			State:   SchedulerOwnershipUnavailable,
			Message: "scheduler is not configured",
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.ownershipState
	if state == "" {
		state = SchedulerOwnershipUnavailable
	}
	if state == SchedulerOwnershipOwner && (!s.schedulerOn || s.riverClient == nil) {
		return SchedulerOwnership{
			State:   SchedulerOwnershipUnavailable,
			Message: "scheduler workers are not running",
		}
	}
	return SchedulerOwnership{State: state, Message: s.ownerMessage}
}

func (s *Service) setOwnershipLocked(state SchedulerOwnershipState, message string) {
	s.ownershipState = state
	s.ownerMessage = strings.TrimSpace(message)
}

// RequireOwner rejects work that would change schedule configuration or queue
// state unless this process currently owns scheduler.lock and River is running.
func (s *Service) RequireOwner() error {
	ownership := s.Ownership()
	if ownership.State == SchedulerOwnershipOwner {
		return nil
	}
	if ownership.Message == "" {
		ownership.Message = "scheduler is not available in this process"
	}
	return fmt.Errorf("%w: %s", ErrSchedulerNotOwner, ownership.Message)
}
