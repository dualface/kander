package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/server"
)

func TestConfirmCLI(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(remote.EnvClientHome, t.TempDir())
	t.Setenv(remote.EnvExecutionID, "")
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "user.name")
	t.Setenv("GIT_CONFIG_VALUE_0", "Local name")
	const hostActor = "Host identity"
	svc, err := server.Open(server.Options{
		DataDir: t.TempDir(), Logger: slog.New(slog.DiscardHandler),
		Hooks: server.Hooks{Attribution: func(context.Context) (string, bool) { return hostActor, true }},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	var requests atomic.Int64
	handler := svc.Handler()
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(web.Close)
	cfg := config.DefaultConfig()
	cfg.Server.URL, cfg.Project.Name, cfg.Language = web.URL, "confirm-cli", "en"
	cfg.WelcomeComplete = true
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	c, err := client.New(web.URL, "cli-fixture")
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	info, err := c.Register(ctx, cfg.Project.Name, "")
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project(info.ID)
	newCard := func(slug, description string) api.Task {
		t.Helper()
		card, err := p.NewTask(ctx, "", api.NewTask{Type: "feature", Slug: slug, Title: "CLI fixture", Description: description})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(card.Text, "<FILL_IN>", "filled")
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
		card, err = p.PutDocument(ctx, "", card.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return card
	}
	card := newCard("request", "Original request")
	rev := strconv.FormatUint(card.Revision, 10)

	before := requests.Load()
	requireFailure(t, runKander(t, "confirm", card.TaskID, "--json"), "missing revision", "usage", 2)
	if requests.Load() != before {
		t.Fatal("missing required revision sent a request")
	}
	t.Setenv("GIT_CONFIG_VALUE_0", "")
	result := runKander(t, "confirm", card.TaskID, "--expect-revision", rev)
	if result.code != 1 || !strings.Contains(result.stderr, "user.name") || requests.Load() != before {
		t.Fatalf("blank Git name sent a request or gave the wrong result: %+v", result)
	}
	t.Setenv("GIT_CONFIG_VALUE_0", "Local name")
	for _, args := range [][]string{{"pick", card.TaskID}, {"move", card.TaskID, "todo"}} {
		result := runKander(t, args...)
		if result.code != 1 || !strings.Contains(result.stderr, "kander confirm") {
			t.Fatalf("%v gave %+v instead of confirmation guidance", args, result)
		}
	}
	if after, err := p.Show(ctx, card.TaskID); err != nil || after.State != "backlog" || after.Revision != card.Revision {
		t.Fatalf("failed commands changed the card: %s, %d, %v", after.State, after.Revision, err)
	}

	result = runKander(t, "confirm", card.TaskID, "--expect-revision", rev, "--json")
	jsonResult := requireResult(t, result, "confirm JSON", "task_id", "state", "location", "revision", "confirmed_by")
	locationPrefix := "kander-server:" + cfg.Project.Name + "/"
	if jsonResult["task_id"] != card.TaskID || jsonResult["state"] != "todo" || jsonResult["location"] != locationPrefix+card.TaskID ||
		jsonResult["revision"] != float64(card.Revision+1) || jsonResult["confirmed_by"] != hostActor {
		t.Fatalf("unexpected confirmation result: %v", jsonResult)
	}
	after, err := p.Show(ctx, card.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if actor, ok := board.LastConfirmer(after.Text); !ok || actor != jsonResult["confirmed_by"] {
		t.Fatalf("confirmation output differs from the stored actor %q", actor)
	}

	textCard := newCard("request-text", "Another request")
	result = runKander(t, "confirm", textCard.TaskID, "--expect-revision", strconv.FormatUint(textCard.Revision, 10)).require(t, "confirm text")
	if result.stdout != locationPrefix+textCard.TaskID+"\n" {
		t.Fatalf("confirmation location = %q", result.stdout)
	}
	ordinary := newCard("ordinary", "")
	runKander(t, "pick", ordinary.TaskID).require(t, "ordinary pick")
	if after, err := p.Show(ctx, ordinary.TaskID); err != nil || after.State != "todo" {
		t.Fatalf("ordinary pick did not enter todo: %s, %v", after.State, err)
	}
}
