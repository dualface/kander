package server_test

import (
	"testing"

	"github.com/dualface/kander/internal/api"
)

// The cleanup scope is empty until a standalone card is done; a group answers
// for its members, and identities are validated before anything is read.
func TestEvidenceCleanupScopeWaitsForCompletion(t *testing.T) {
	f := newFixture(t)
	p := f.project("host-a")
	task := todoTask(t, p, "cleanup-scope")
	if value, err := p.TaskEvidenceCleanup(t.Context(), task.TaskID); err != nil || value.Ready || len(value.Tasks) != 0 || len(value.Sources) != 0 {
		t.Fatalf("scope of a todo card: %+v %v", value, err)
	}
	member := todoTask(t, p, "cleanup-member", "20261125-cleanup-scope-group")
	if value, err := p.TaskEvidenceCleanup(t.Context(), member.TaskID); err != nil || value.Ready {
		t.Fatalf("scope of an unfinished group member: %+v %v", value, err)
	}
	if value, err := p.GroupEvidenceCleanup(t.Context(), "20261125-cleanup-scope-group"); err != nil || value.Ready {
		t.Fatalf("scope of an unfinished group: %+v %v", value, err)
	}
	if _, err := p.GroupEvidenceCleanup(t.Context(), "Not-A-Group"); !api.IsCode(err, api.CodeInvalidRequest) {
		t.Fatalf("invalid group identity: %v", err)
	}
	if _, err := p.TaskEvidenceCleanup(t.Context(), "20991231-missing-task"); err == nil {
		t.Fatal("a missing card answered")
	}
}
