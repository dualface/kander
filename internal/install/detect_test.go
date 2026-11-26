package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLooksLikeModule(t *testing.T) {
	tests := []struct {
		name      string
		firstLine string
		want      bool
	}{
		{name: "Kander v2", firstLine: "module github.com/dualface/kander/v2", want: true},
		{name: "unrelated module", firstLine: "module example.com/other", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tt.firstLine+"\n\ngo 1.25.0\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := looksLikeModule(dir); got != tt.want {
				t.Errorf("looksLikeModule with first line %q = %t, want %t", tt.firstLine, got, tt.want)
			}
		})
	}
}
