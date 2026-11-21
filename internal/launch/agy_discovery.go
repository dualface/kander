package launch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/fs"
)

func agyBrainRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "brain"), nil
}

func agySessionIDs(root string) (map[string]struct{}, error) {
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ids := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && sessionReferenceRe.MatchString(entry.Name()) {
			ids[entry.Name()] = struct{}{}
		}
	}
	return ids, nil
}

func agySessionSnapshot() (map[string]struct{}, error) {
	root, err := agyBrainRoot()
	if err != nil {
		return nil, err
	}
	return agySessionIDs(root)
}

// The first user request carries the unique task-file path. A workspace's
// most recent conversation can belong to a different concurrent launch.
func agyTranscriptMatches(root, id, prompt string) (bool, error) {
	path := filepath.Join(root, id, ".system_generated", "logs", "transcript.jsonl")
	file, err := fs.OpenRegularFileIfExists(root, path)
	if file == nil || err != nil {
		return false, err
	}
	defer file.Close()
	// The first JSON line includes client metadata in addition to the prompt.
	reader := bufio.NewReaderSize(file, 256<<10)
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, io.EOF) || errors.Is(err, bufio.ErrBufferFull) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var step struct{ Source, Type, Content string }
	if json.Unmarshal(line, &step) != nil || step.Source != "USER_EXPLICIT" || step.Type != "USER_INPUT" {
		return false, nil
	}
	content, ok := strings.CutPrefix(step.Content, "<USER_REQUEST>\n")
	request, _, closed := strings.Cut(content, "\n</USER_REQUEST>")
	return ok && closed && strings.TrimPrefix(request, "/plan ") == prompt, nil
}

func discoverAgySession(previous map[string]struct{}, prompt string, wait time.Duration) (string, error) {
	root, err := agyBrainRoot()
	if err != nil {
		return "", err
	}
	return waitForSessionCandidates(wait, func(ctx context.Context) ([]string, error) {
		ids, err := agySessionIDs(root)
		if err != nil {
			return nil, err
		}
		var matches []string
		var readErr error
		for id := range ids {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if _, existed := previous[id]; existed {
				continue
			}
			match, err := agyTranscriptMatches(root, id, prompt)
			if err != nil {
				readErr = err
			} else if match {
				matches = append(matches, id)
			}
		}
		if len(matches) == 0 {
			return nil, readErr
		}
		return matches, nil
	})
}
