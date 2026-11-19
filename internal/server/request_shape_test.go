package server

import (
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func TestRequestShapeAcceptsAndRefuses(t *testing.T) {
	wrapUp := &board.DispatchInput{ID: "d", Kind: "wrap-up"}
	syncDispatch := &board.DispatchInput{ID: "d", Kind: "sync"}
	valid := func(kind string, dispatch *board.DispatchInput) api.NewCollabRequest {
		return api.NewCollabRequest{Kind: kind, Message: "hello", FromClient: "host-a", Dispatch: dispatch}
	}
	cases := []struct {
		name    string
		input   api.NewCollabRequest
		refused string
	}{
		{"note", valid(api.RequestNote, nil), ""},
		{"wrap-up", valid(api.RequestWrapUp, wrapUp), ""},
		{"fix", valid(api.RequestFix, nil), ""},
		{"sync", valid(api.RequestSync, nil), ""},
		{"review", valid(api.RequestReview, nil), ""},
		{"stop", valid(api.RequestStop, nil), ""},
		{"sync dispatch", valid(api.RequestSync, syncDispatch), ""},
		{"fix kind of a sync dispatch", valid(api.RequestFix, syncDispatch), ""},
		// An older client sends a wrap-up proposal as kind sync.
		{"legacy wrap-up proposal", valid(api.RequestSync, wrapUp), ""},
		{"note with dispatch", valid(api.RequestNote, syncDispatch), "a note request carries no dispatch"},
		{"wrap-up without dispatch", valid(api.RequestWrapUp, nil), "a wrap-up request needs a wrap-up dispatch"},
		{"wrap-up with sync dispatch", valid(api.RequestWrapUp, syncDispatch), "a wrap-up request needs a wrap-up dispatch"},
		{"unknown kind", valid("surprise", nil), `unknown request kind "surprise"`},
		{"empty message", api.NewCollabRequest{Kind: api.RequestNote, Message: " \t\n", FromClient: "host-a"}, "request needs a message"},
		{"long message", api.NewCollabRequest{Kind: api.RequestNote, Message: strings.Repeat("a", api.MaxRequestMessage+1), FromClient: "host-a"}, "request message exceeds 64 KiB"},
		{"bad client", api.NewCollabRequest{Kind: api.RequestNote, Message: "hello", FromClient: "bad client"}, "request needs a valid client identity"},
	}
	for _, tc := range cases {
		err := checkRequestShape(tc.input)
		if tc.refused == "" {
			if err != nil {
				t.Errorf("%s: refused: %v", tc.name, err)
			}
			continue
		}
		if !api.IsCode(err, api.CodeInvalidRequest) {
			t.Errorf("%s: want invalid_request, got %v", tc.name, err)
			continue
		}
		var failure *api.Error
		if !errors.As(err, &failure) || failure.Message != tc.refused {
			t.Errorf("%s: message %v, want %q", tc.name, err, tc.refused)
		}
		// The combined message of the older server must not come back: the
		// client takes it as "this server does not know the kind".
		if strings.Contains(failure.Message, "request needs kind, client and a message") {
			t.Errorf("%s: refused with the legacy combined message", tc.name)
		}
	}
}
