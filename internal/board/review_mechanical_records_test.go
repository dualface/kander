package board

import (
	"reflect"
	"testing"
)

func TestReviewRoleFixedRecordsUsesLatestSuccessfulRoleRecords(t *testing.T) {
	old := ReviewDisposition{RecordID: "old", FindingID: "M-1", TaskID: "task-a", Status: "fixed"}
	deferred := ReviewDisposition{RecordID: "new", FindingID: "M-1", TaskID: "task-a", Status: "deferred", PreviousRecordID: "old"}
	latest := ReviewDisposition{RecordID: "latest", FindingID: "M-2", TaskID: "task-a", Status: "fixed"}
	otherTask := ReviewDisposition{RecordID: "shared", FindingID: "M-1", TaskID: "task-b", Status: "fixed"}
	prior := ReviewDisposition{RecordID: "prior", FindingID: "L-1", TaskID: "task-a", Status: "fixed"}
	v := ReviewBatchView{Runs: []ReviewRunView{
		{Run: ReviewRun{ReviewInput: ReviewInput{RunID: "prior", Role: "PMQA"}, ExecutionStatus: "ok"}, Records: []ReviewDisposition{prior}},
		{Run: ReviewRun{ReviewInput: ReviewInput{RunID: "current", Role: "PMQA"}, ExecutionStatus: "ok"}, Records: []ReviewDisposition{old, latest, deferred, otherTask}},
		{Run: ReviewRun{ReviewInput: ReviewInput{RunID: "failed", Role: "PMQA"}, ExecutionStatus: "failed"}, Records: []ReviewDisposition{old}},
		{Run: ReviewRun{ReviewInput: ReviewInput{RunID: "other", Role: "Security"}, ExecutionStatus: "ok"}, Records: []ReviewDisposition{old}},
	}}
	if got := ReviewRoleFixedRecords(v, "PMQA"); !reflect.DeepEqual(got, []ReviewDisposition{prior, otherTask, latest}) {
		t.Fatalf("latest successful role records: %+v", got)
	}
}
