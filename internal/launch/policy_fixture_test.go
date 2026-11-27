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
	values, err := json.Marshal(config.PolicyValuesOf(config.DefaultConfig()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Project.PutPolicy(t.Context(), client.NewKey(), api.PolicyWrite{Values: values}); err != nil {
		t.Fatal(err)
	}
}
