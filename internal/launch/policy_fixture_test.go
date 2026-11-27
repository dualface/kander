package launch

import (
	"encoding/json"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// initTestPolicy gives the fixture project a project policy with the default values, so the
// commands a test runs carry no project_policy_unset remark and their output stays the one the
// test pins.
func initTestPolicy(t *testing.T, session *remote.Session) {
	t.Helper()
	putTestPolicy(t, session, config.DefaultConfig())
}

// putTestPolicy makes the policy values of cfg the project policy: the policy, not the local
// configuration, decides them once a project has one.
func putTestPolicy(t *testing.T, session *remote.Session, cfg *config.Config) {
	t.Helper()
	current, err := session.Project.Policy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	values, err := json.Marshal(config.PolicyValuesOf(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Project.PutPolicy(t.Context(), client.NewKey(), api.PolicyWrite{ExpectedRevision: current.Revision, Values: values}); err != nil {
		t.Fatal(err)
	}
}
