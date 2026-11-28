package remote

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkerRecordRoundTrips(t *testing.T) {
	home := t.TempDir()
	started := time.Date(2026, 11, 19, 8, 30, 0, 0, time.FixedZone("east", 8*3600))
	want := WorkerRecord{Version: "v2.0.0-dev+abc1234", PID: 4242, Executable: "/usr/local/bin/kander", Handover: true, Managed: true, StartedAt: started}
	if err := WriteWorkerRecord(home, want); err != nil {
		t.Fatal(err)
	}
	got, state, err := ReadWorkerRecord(home)
	if err != nil || state != WorkerRecordRecorded {
		t.Fatalf("state %v, error %v", state, err)
	}
	want.Schema, want.StartedAt = 1, started.UTC()
	if got != want {
		t.Fatalf("record %+v, want %+v", got, want)
	}
	data, err := os.ReadFile(filepath.Join(home, WorkerRecordName))
	if err != nil {
		t.Fatal(err)
	}
	const text = `{"schema":1,"version":"v2.0.0-dev+abc1234","pid":4242,"executable":"/usr/local/bin/kander","handover":true,"managed":true,"started_at":"2026-11-19T00:30:00Z"}` + "\n"
	if string(data) != text {
		t.Fatalf("the file has the seven documented fields in UTC:\n%s", data)
	}
}

func TestWorkerRecordReadRules(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		write         bool
		want          WorkerRecordState
		wantVersion   string
	}{
		{name: "no file", want: WorkerRecordMissing},
		{name: "unknown fields are accepted", write: true,
			content: `{"schema":1,"version":"v1","pid":1,"executable":"","handover":false,"started_at":"2026-11-19T00:00:00Z","added_later":{"x":1}}`,
			want:    WorkerRecordRecorded, wantVersion: "v1"},
		{name: "another schema", write: true, content: `{"schema":2,"version":"v1"}`, want: WorkerRecordUnknown},
		{name: "no schema", write: true, content: `{"version":"v1"}`, want: WorkerRecordUnknown},
		{name: "invalid JSON", write: true, content: `{"schema":1,`, want: WorkerRecordUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.write {
				if err := os.WriteFile(filepath.Join(home, WorkerRecordName), []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, state, err := ReadWorkerRecord(home)
			if err != nil || state != tc.want || got.Version != tc.wantVersion {
				t.Fatalf("state %v version %q error %v, want state %v version %q", state, got.Version, err, tc.want, tc.wantVersion)
			}
		})
	}
}
