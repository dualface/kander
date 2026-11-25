package board

import (
	"strings"
	"testing"
)

func TestPersonFieldValidation(t *testing.T) {
	for _, tc := range []struct {
		name, value, want string
		invalid           bool
	}{
		{name: "empty"},
		{name: "trim Unicode whitespace", value: "　林 美玲 ", want: "林 美玲"},
		{name: "byte limit", value: strings.Repeat("界", 85) + "x", want: strings.Repeat("界", 85) + "x"},
		{name: "too many bytes", value: strings.Repeat("界", 86), invalid: true},
		{name: "newline at edge", value: "Alice\n", invalid: true},
		{name: "tab at edge", value: "\tAlice", invalid: true},
		{name: "Unicode control", value: "Al\u0085ice", invalid: true},
		{name: "Unicode line separator", value: "Al\u2028ice", invalid: true},
		{name: "Unicode paragraph separator", value: "Alice\u2029", invalid: true},
		{name: "invalid UTF-8", value: "\xff", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := personField(FieldAssignee, tc.value)
			if got != tc.want || (err != nil) != tc.invalid {
				t.Fatalf("normalized value = %q, error = %v", got, err)
			}
		})
	}
}

func TestAssignmentPreservesOtherBytes(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "assignment-crlf", false)
	before := strings.Replace(s.Text, "- ASSIGNEE:\n", "- ASSIGNEE:\r\n", 1)
	before += "\n- ASSIGNEE: body text, not metadata\n"
	if err := WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}}, func(tx *Transaction) error {
		return tx.Put(s.Entry.TaskID, "spec.md", before)
	}); err != nil {
		t.Fatal(err)
	}
	if err := Assign(root, s.Entry.TaskID, "Alice", nil, TransactionExtension{}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(before, "- ASSIGNEE:\r\n", "- ASSIGNEE: Alice\r\n", 1)
	if got := transactionSnapshot(t, root, s.Entry.TaskID).Text; got != want {
		t.Fatalf("assignment changed more than the header: %q", got)
	}
}
