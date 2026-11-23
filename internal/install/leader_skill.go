package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/skills"
)

const leaderName = "kander-leader"
const leaderReceipt = ".kander-install.json"

// Publish the entry last, after all of its references are available.
var leaderFiles = []string{"references/deployment.md", "references/handover.md", "references/server-mode.md", "SKILL.md"}

type skillReceipt struct {
	Scope string            `json:"scope"`
	Files map[string]string `json:"files"`
}

type leaderSkill struct {
	dir, anchor string
	exists      bool
	managed     bool
	receipt     skillReceipt
}

func leaderTarget(agent string, paths config.InstallPaths) (string, error) {
	base := paths.ProjectRoot
	if paths.Mode != config.ModeProject {
		var err error
		base, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	switch agent {
	case "codex":
		base = filepath.Join(base, ".agents")
	case "claude":
		if paths.Mode == config.ModeProject {
			base = filepath.Join(base, ".claude")
		} else {
			root, _, err := agentConfigRoot(agent, paths)
			if err != nil {
				return "", err
			}
			// A global install covers an existing Claude installation.
			if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
				return "", nil
			} else if err != nil {
				return "", err
			}
			base = root
		}
	default:
		return "", nil
	}
	return filepath.Join(base, "skills", leaderName), nil
}

// A directory that existed before installation, including a user's link to the
// shared skill, is never adopted. Only a receipt for this scope establishes ownership.
func inspectLeaderSkill(dir string, paths config.InstallPaths) (leaderSkill, error) {
	s := leaderSkill{dir: dir}
	var err error
	s.anchor, err = fileAnchor(dir)
	if err != nil {
		return s, err
	}
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.exists = true
	if !info.IsDir() || fs.IsReparsePoint(dir) {
		return s, nil
	}
	data, found, err := fs.ReadRegularFileIfExists(s.anchor, filepath.Join(dir, leaderReceipt))
	if err != nil || !found {
		return s, err
	}
	s.managed = json.Unmarshal(data, &s.receipt) == nil && s.receipt.Scope == paths.RulesDir && s.receipt.Files != nil
	return s, nil
}

func (s leaderSkill) planFiles() ([]PlannedFile, error) {
	var files []PlannedFile
	for _, name := range leaderFiles {
		target := filepath.Join(s.dir, filepath.FromSlash(name))
		file := PlannedFile{Target: target, Change: RulesChangeNone}
		switch {
		case !s.exists:
			file.Change = RulesChangeCreate
		case s.managed:
			got, found, err := fs.ReadRegularFileIfExists(s.anchor, target)
			if err != nil {
				return nil, err
			}
			want, err := skills.Leader.ReadFile(leaderName + "/" + name)
			if err != nil {
				return nil, err
			}
			switch {
			case !found:
				file.Change = RulesChangeCreate
			case !bytes.Equal(got, want) && fileHash(got) == s.receipt.Files[name]:
				file.Change = RulesChangeUpdate
			}
		}
		files = append(files, file)
	}
	return files, nil
}

func leaderPreview(agent string, paths config.InstallPaths) []PlannedFile {
	dir, err := leaderTarget(agent, paths)
	if dir == "" && err == nil {
		return nil
	}
	if err == nil {
		var skill leaderSkill
		skill, err = inspectLeaderSkill(dir, paths)
		if err == nil {
			var files []PlannedFile
			files, err = skill.planFiles()
			if err == nil {
				return files
			}
		}
	}
	return []PlannedFile{{Target: dir, Err: err}}
}

func installLeaderSkills(paths config.InstallPaths) error {
	switches, err := config.ScopeInstallRules(paths)
	if err != nil {
		return err
	}
	for _, agent := range []string{"codex", "claude"} {
		if !switches.Enabled(agent) {
			continue
		}
		dir, err := leaderTarget(agent, paths)
		if err != nil {
			return err
		}
		if dir == "" {
			continue
		}
		s, err := inspectLeaderSkill(dir, paths)
		if err != nil {
			return err
		}
		if !s.exists || s.managed {
			if err := s.install(paths); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s leaderSkill) install(paths config.InstallPaths) error {
	stamp := filepath.Join(s.dir, leaderReceipt)
	if !s.exists {
		if err := fs.EnsureInheritedDirectoryPath(filepath.Dir(s.dir)); err != nil {
			return err
		}
		s.receipt = skillReceipt{Scope: paths.RulesDir, Files: map[string]string{}}
		initial, err := json.Marshal(s.receipt)
		if err != nil {
			return err
		}
		if err := fs.CreateDirectoryWithTextFile(s.anchor, s.dir, leaderReceipt, string(initial)); err != nil {
			return err
		}
		s.exists, s.managed = true, true
	}
	files, err := s.planFiles()
	if err != nil {
		return err
	}
	previous := maps.Clone(s.receipt.Files)
	for i, file := range files {
		name := leaderFiles[i]
		data, err := skills.Leader.ReadFile(leaderName + "/" + name)
		if err != nil {
			return err
		}
		if file.Change != RulesChangeNone {
			if err := fs.EnsureInheritedDirectoryPath(filepath.Dir(file.Target)); err != nil {
				return err
			}
			if err := fs.WriteBytesAtomicInherited(s.anchor, file.Target, data, file.Change == RulesChangeUpdate); err != nil {
				return err
			}
		} else {
			// Recover a write completed before the receipt was saved, without
			// claiming locally changed bytes.
			current, err := fs.ReadRegularFile(s.anchor, file.Target)
			if err != nil {
				return err
			}
			if !bytes.Equal(current, data) {
				continue
			}
		}
		s.receipt.Files[name] = fileHash(data)
	}
	if maps.Equal(previous, s.receipt.Files) {
		return nil
	}
	record, err := json.Marshal(s.receipt)
	if err != nil {
		return err
	}
	return fs.WriteBytesAtomicInherited(s.anchor, stamp, record, true)
}

// Apply only to named payload files with the recorded installed digest. Never
// use paths from a receipt. Keep a modified skill together with its references.
func planLeaderRemoval(dir string, paths config.InstallPaths) ([]UninstallItem, []string) {
	keep := func(reason string) ([]UninstallItem, []string) {
		return []UninstallItem{{Action: UninstallKeep, Path: dir, Reason: reason}}, nil
	}
	s, err := inspectLeaderSkill(dir, paths)
	if err != nil {
		return keep(err.Error())
	}
	if !s.exists {
		return nil, nil
	}
	if !s.managed {
		return keep(config.Text("install.uninstall_not_kander_file"))
	}
	var items []UninstallItem
	for _, name := range leaderFiles {
		target := filepath.Join(dir, filepath.FromSlash(name))
		data, found, err := fs.ReadRegularFileIfExists(s.anchor, target)
		if err != nil {
			return keep(err.Error())
		}
		if !found {
			continue
		}
		if fileHash(data) != s.receipt.Files[name] {
			return keep(config.Text("install.leader_skill_modified"))
		}
		items = append(items, UninstallItem{Action: UninstallRemove, Path: target, skillDigest: fileHash(data)})
	}
	stamp := filepath.Join(dir, leaderReceipt)
	data, err := fs.ReadRegularFile(s.anchor, stamp)
	if err != nil {
		return keep(err.Error())
	}
	items = append(items, UninstallItem{Action: UninstallRemove, Path: stamp, skillDigest: fileHash(data)})
	return items, []string{filepath.Join(dir, "references"), dir}
}

func verifySkillRemoval(item UninstallItem) error {
	anchor, err := fileAnchor(item.Path)
	if err != nil {
		return err
	}
	current, err := fs.ReadRegularFile(anchor, item.Path)
	if err != nil {
		return err
	}
	if fileHash(current) != item.skillDigest {
		return fmt.Errorf("%s: %s", item.Path, config.Text("install.leader_skill_modified"))
	}
	return nil
}

func removeSkillFile(item UninstallItem) error {
	if err := verifySkillRemoval(item); err != nil {
		return err
	}
	anchor, err := fileAnchor(item.Path)
	if err != nil {
		return err
	}
	_, err = fs.RemoveRegularFileIfExists(anchor, item.Path)
	return err
}
