package server

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
)

// Attachment names with a space or a non-ASCII character cross the client and
// the server escaped exactly once, so they can be published and read back.
func TestDocumentNamesNeedingEscapesRoundTrip(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	srv, httpServer, project := openCreationServer(t, t.TempDir(), func() time.Time { return now })
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	card, err := project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "document-names", Title: "Document names", Language: "en", Large: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"notes/a b.md", "说明/计划.md", "a#b%c.md"} {
		text := "# " + name + "\n"
		sum := sha256.Sum256([]byte(text))
		card, err = project.PutDocument(t.Context(), "", card.TaskID, api.Document{Path: name, Text: text, SHA256: hex.EncodeToString(sum[:]), ExpectedRevision: card.Revision}, nil)
		if err != nil {
			t.Fatalf("publish %q: %v", name, err)
		}
		got, err := project.Document(t.Context(), card.TaskID, name)
		if err != nil || got.Text != text || got.Path != name {
			t.Fatalf("read %q: %+v %v", name, got, err)
		}
	}
}
