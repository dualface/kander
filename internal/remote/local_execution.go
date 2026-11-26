package remote

import (
	"errors"
	"fmt"
	"os"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
)

// Refusal conditions of the retained-execution checks. Each names one catalog
// key below "remote.local_execution." that states the failed condition and
// the values it was judged on.
const (
	RefusalNoClaim            = "no_claim"
	RefusalInvalidIdentity    = "invalid_identity"
	RefusalForeignClient      = "foreign_client"
	RefusalBoundExecution     = "bound_execution"
	RefusalCoordinatorNoGroup = "coordinator_no_group"
	RefusalCoordinatorHolder  = "coordinator_not_holder"
	RefusalRetainedMismatch   = "retained_mismatch"
	RefusalSceneMismatch      = "scene_mismatch"
	RefusalSelfClaimed        = "self_claimed"
	RefusalRecordMismatch     = "record_mismatch"
	RefusalSessionInvalid     = "session_invalid"
	RefusalTaskChanged        = "task_changed"
	RefusalPaneStale          = "pane_stale"
)

// Reasons a coordinator holds no valid group credential, each one catalog
// key "remote.local_execution.reason_<name>".
const (
	reasonMissing    = "missing"
	reasonNotGroup   = "not_group"
	reasonLost       = "lost"
	reasonMismatch   = "mismatch"
	reasonDamaged    = "damaged"
	reasonUnreadable = "unreadable"
)

// RefusalText renders a fragment a refusal condition embeds, catalog key
// "remote.local_execution.<name>".
func RefusalText(name string, args ...any) string {
	return config.Text("remote.local_execution."+name, args...)
}

func reasonText(name string, args ...any) string {
	return RefusalText("reason_"+name, args...)
}

// LocalExecutionRefused builds the refusal of an operation that needs the
// exact retained execution on this host. It keeps the common sentence and
// appends the failed condition with its values; the values never include a
// credential token.
func LocalExecutionRefused(condition string, args ...any) error {
	return errors.New(config.Text("remote.local_execution_refused", config.Text("remote.local_execution_required"), RefusalText(condition, args...)))
}

// RefusalValue renders a value for a refusal, localizing the empty one.
func RefusalValue(value string) string {
	if value == "" {
		return RefusalText("value_empty")
	}
	return value
}

// allowCoordinator decides whether a process bound to another execution may
// resolve task: only a coordinator process, for a member of a group whose
// valid group credential this host holds.
func (s *Session) allowCoordinator(task string, text string, claimExecution, bound string) error {
	coordinator := os.Getenv(EnvCoordinatorExecutionID)
	if coordinator == "" || coordinator != bound {
		return LocalExecutionRefused(RefusalBoundExecution, bound, claimExecution, RefusalValue(coordinator))
	}
	group := board.TaskGroupFrom(text)
	if group == "" {
		return LocalExecutionRefused(RefusalCoordinatorNoGroup, bound, task)
	}
	credential, found, err := s.credentialFor(group, bound)
	reason := ""
	var fault *credentialFault
	switch {
	case errors.As(err, &fault) && fault.kind == faultIdentity:
		reason = reasonText(reasonMismatch)
	case errors.As(err, &fault):
		reason = reasonText(reasonDamaged)
	case err != nil:
		reason = reasonText(reasonUnreadable)
	case !found:
		reason = reasonText(reasonMissing)
	case credential.ResourceKind != "group":
		reason = reasonText(reasonNotGroup)
	case credential.Lost:
		reason = reasonText(reasonLost, RefusalValue(credential.LostReason))
	}
	if reason != "" {
		return LocalExecutionRefused(RefusalCoordinatorHolder, group, bound, reason)
	}
	return nil
}

func quoted(value string) string { return fmt.Sprintf("%q", value) }
