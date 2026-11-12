package remote

import (
	"context"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

type sceneService struct {
	ProjectService
	task api.Task
}

func (s sceneService) Show(context.Context, string) (api.Task, error) { return s.task, nil }
func (s sceneService) List(context.Context, string) (api.TaskList, error) {
	return api.TaskList{Tasks: []api.TaskSummary{{TaskID: s.task.TaskID, State: s.task.State, Claim: s.task.Claim, Session: board.MetadataFrom(s.task.Text, board.FieldSession), Window: board.MetadataFrom(s.task.Text, board.FieldWindow)}}}, nil
}
func TestRecoveredAuditGenerationNeverExposesThePredecessorSceneAsLocal(t *testing.T) {
	connection, err := client.New("http://example.invalid", "host-a")
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{Client: connection, ServerURL: "http://example.invalid", Info: api.Project{ID: "project-scene", Name: "scene"}, Home: t.TempDir()}
	task := api.Task{TaskID: "20261112-scene-recovery-task", State: "working", Text: "# Scene\n\n- OWNER: codex\n- WINDOW: tmux:$1:@1:%1\n- SESSION: codex scene\n", Claim: &api.ClaimView{ClientID: "host-a", ExecutionID: "original-execution", Generation: 1, Status: api.ClaimActive}}
	credential := Credential{ServerURL: session.ServerURL, ClientID: connection.ClientID(), ProjectID: session.Info.ID, TaskID: task.TaskID, ExecutionID: task.Claim.ExecutionID, Generation: 1, Token: "private-proof", Window: "tmux:$1:@1:%1", Session: "codex scene"}
	if err := session.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	if !session.SceneIsLocal(task.TaskID, task.Claim, credential.Window, credential.Session) {
		t.Fatal("the actual saved scene was hidden")
	}
	if session.SceneIsLocal(task.TaskID, task.Claim, "tmux:$9:@9:%9", credential.Session) {
		t.Fatal("changed routing was accepted")
	}
	if err := RemoveCredential(session.Home, credential); err != nil {
		t.Fatal(err)
	}
	task.Claim.Status = api.ClaimReleased
	if !session.SceneIsLocal(task.TaskID, task.Claim, credential.Window, credential.Session) {
		t.Fatal("normal final cleanup lost its retained scene")
	}
	// Audited recovery retains old card metadata, but creates no new scene.
	task.Claim = &api.ClaimView{ClientID: "host-a", ExecutionID: "released-audit-generation", Generation: 2, Status: api.ClaimReleased}
	session.Project = sceneService{task: task}
	if session.SceneIsLocal(task.TaskID, task.Claim, credential.Window, credential.Session) {
		t.Fatal("an audit generation borrowed the predecessor scene")
	}
	view, err := session.ListView(t.Context(), false)
	if err != nil || view.Tasks[0].Window != "" || view.Tasks[0].Session != "" {
		t.Fatalf("board exposed an unowned scene: %+v %v", view, err)
	}
	detail, err := session.TaskView(t.Context(), task.TaskID)
	if err != nil || detail.Window != "" || detail.Session != "" {
		t.Fatalf("detail exposed an unowned scene: %+v %v", detail, err)
	}
	list, err := session.Project.List(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	machine, err := session.machineList(t.Context(), list, func(context.Context, []board.AgentStatusRequest) []board.ListAgent {
		t.Fatal("unowned scene was probed on this host")
		return nil
	})
	if err != nil || machine.Tasks[0].Window != "" || machine.Tasks[0].Session != "" {
		t.Fatalf("dsh exposed an unowned scene: %+v %v", machine, err)
	}
	if board.MetadataFrom(task.Text, board.FieldWindow) != credential.Window {
		t.Fatal("routing filter changed formal scene evidence")
	}
}
