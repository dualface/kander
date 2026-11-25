package remote

import (
	"errors"
	"strings"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/config"
)

// legacyRequestRejection is the one message a server that predates the note and
// wrap-up kinds gives for every malformed request.
const legacyRequestRejection = "request needs kind, client and a message of at most 64 KiB"

// CheckRequestMessage refuses a message the server would refuse, before any
// request or receipt exists. With it, the legacy rejection of a note or wrap-up
// request can only mean that the server does not know the kind.
func CheckRequestMessage(message string) error {
	switch {
	case strings.TrimSpace(message) == "":
		return errors.New(config.Text("remote.request_message_empty"))
	case len(message) > api.MaxRequestMessage:
		return errors.New(config.Text("remote.request_message_too_long", api.MaxRequestMessage>>10))
	}
	return nil
}

// RequestCreateError replaces the legacy rejection of a note or wrap-up request
// by an instruction to upgrade the server first. The error code stays, so the
// failure is still a known refusal and the same command can run again.
func RequestCreateError(kind string, err error) error {
	var failure *api.Error
	if err == nil || kind != api.RequestNote && kind != api.RequestWrapUp || !errors.As(err, &failure) || failure.Code != api.CodeInvalidRequest || failure.Message != legacyRequestRejection {
		return err
	}
	return &api.Error{Code: failure.Code, Message: config.Text("remote.request_kind_server_upgrade", kind), Status: failure.Status}
}
