package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
)

// reviewCall sends one review write with claim to a server that answers
// refusal, and returns the request header and the error.
func reviewCall(t *testing.T, claim *Claim, refusal api.Error) (http.Header, error) {
	t.Helper()
	var seen http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(refusal)
	}))
	t.Cleanup(server.Close)
	c, err := New(server.URL, "host-c")
	if err != nil {
		t.Fatal(err)
	}
	c.Retries = 0
	_, err = c.Project("demo").ReviewPlan(t.Context(), "key", map[string]string{}, claim)
	return seen, err
}

// The declaration travels as one header, a missing one sends none, and an
// empty one is sent as it is so the server refuses it.
func TestCoordinatorBasisHeader(t *testing.T) {
	basis := api.CoordinatorBasis{{TaskID: "20261120-first-task", Revision: 4, Generation: 2}}
	header, _ := reviewCall(t, &Claim{Token: "group-token", Generation: 3, CoordinatorBasis: basis}, api.Error{Code: api.CodeInvalidRequest, Message: "x"})
	var got api.CoordinatorBasis
	if err := json.Unmarshal([]byte(header.Get(api.HeaderCoordinatorBasis)), &got); err != nil || len(got) != 1 || got[0] != basis[0] {
		t.Fatalf("declaration: %q %v", header.Get(api.HeaderCoordinatorBasis), err)
	}
	if header.Get(api.HeaderClaimToken) != "group-token" || header.Get(api.HeaderClaimGen) != "3" {
		t.Fatalf("the group lease proof was not sent: %v", header)
	}
	header, _ = reviewCall(t, &Claim{Token: "card-token", Generation: 1}, api.Error{Code: api.CodeInvalidRequest, Message: "x"})
	if _, present := header[http.CanonicalHeaderKey(api.HeaderCoordinatorBasis)]; present {
		t.Fatalf("a holder proof carried a declaration: %v", header)
	}
	header, _ = reviewCall(t, &Claim{CoordinatorBasis: api.CoordinatorBasis{}}, api.Error{Code: api.CodeInvalidRequest, Message: "x"})
	if values := header.Values(api.HeaderCoordinatorBasis); len(values) != 1 || values[0] != "[]" {
		t.Fatalf("an empty declaration was dropped: %v", header)
	}
}

// A refusal of a coordinator write names the next step and keeps the server's
// code; a refusal that cannot come from the authority is left alone.
func TestCoordinatorRefusalNamesTheNextStep(t *testing.T) {
	claim := &Claim{Token: "group-token", Generation: 3, CoordinatorBasis: api.CoordinatorBasis{{TaskID: "20261120-first-task", Revision: 4, Generation: 2}}, CoordinatorNote: "RUN AGAIN."}
	for _, code := range []string{api.CodeHeldByOther, api.CodeRevisionConflict, api.CodeLeaseInvalid, api.CodeTakenOver, api.CodeEvidenceIncomplete, api.CodeInvalidRequest} {
		_, err := reviewCall(t, claim, api.Error{Code: code, Message: "refused"})
		if !api.IsCode(err, code) || !strings.HasSuffix(err.Error(), "refused RUN AGAIN.") {
			t.Errorf("%s: %v", code, err)
		}
	}
	_, err := reviewCall(t, claim, api.Error{Code: api.CodeNotFound, Message: "refused"})
	if !api.IsCode(err, api.CodeNotFound) || strings.Contains(err.Error(), "RUN AGAIN") {
		t.Errorf("an unrelated refusal was annotated: %v", err)
	}
}
