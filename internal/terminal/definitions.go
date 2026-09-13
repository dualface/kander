package terminal

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// Definition sources in ascending precedence.
const (
	SourceEmbedded = "embedded"
	SourceGlobal   = "global"
	SourceProject  = "project"
)

// DefinitionsDirName is the share subdirectory holding user definitions.
const DefinitionsDirName = "terminals"

// DefinitionReport is the outcome of one terminal definition source: an
// embedded definition, a user definition file, or a user definitions
// directory that could not be listed.
type DefinitionReport struct {
	Source    string
	Path      string
	Name      string
	Launchers []string
	// Active is false when a higher-precedence file of the same name replaced
	// this one.
	Active bool
	Err    error
}

// DefinitionDir is one user definition directory.
type DefinitionDir struct {
	Source string
	// Root is the trusted share directory; Path is its terminals directory.
	Root string
	Path string
}

type loadedDefinition struct {
	source   string
	path     string
	def      *Definition
	backends []*DeclarativeBackend
}

var definitionRegistry struct {
	sync.Mutex
	embedded []*loadedDefinition
	// userLoaded is false until the share directories are read.
	userLoaded bool
	active     []*loadedDefinition
	reports    []DefinitionReport
}

// DefinitionDirs lists the user definition directories in ascending
// precedence. Tests replace it to point at temporary share directories.
var DefinitionDirs = defaultDefinitionDirs

func init() {
	config.RegisterLauncherNameSource(definitionLauncherNames)
}

// defaultDefinitionDirs returns the global share directory and, inside a Git
// repository, the project install share directory of its main worktree.
func defaultDefinitionDirs() []DefinitionDir {
	var dirs []DefinitionDir
	if global, err := config.GlobalInstallPaths(); err == nil {
		dirs = append(dirs, DefinitionDir{Source: SourceGlobal, Root: global.ShareDir, Path: filepath.Join(global.ShareDir, DefinitionsDirName)})
	}
	if cwd, err := os.Getwd(); err == nil {
		if project, err := config.ProjectInstallPaths(cwd); err == nil {
			dirs = append(dirs, DefinitionDir{Source: SourceProject, Root: project.ShareDir, Path: filepath.Join(project.ShareDir, DefinitionsDirName)})
		}
	}
	return dirs
}

// RegisterDefinition registers a built-in definition embedded in the binary.
// It runs from package initialization and pushes the launcher names to
// configuration validation; an invalid or conflicting built-in definition is
// a programming error.
func RegisterDefinition(source string, data []byte) {
	def, err := DecodeDefinition(source, data)
	if err != nil {
		panic(err)
	}
	loaded, err := newLoadedDefinition(SourceEmbedded, source, def)
	if err != nil {
		panic(err)
	}
	definitionRegistry.Lock()
	defer definitionRegistry.Unlock()
	if conflict := launcherConflict(loaded, definitionRegistry.embedded); conflict != "" {
		panic(fmt.Sprintf("terminal: embedded definition %s: %s", source, conflict))
	}
	definitionRegistry.embedded = append(definitionRegistry.embedded, loaded)
	definitionRegistry.userLoaded = false
	config.RegisterLauncherNames(def.LauncherNames()...)
}

// LauncherNames returns the launcher names a definition provides, sorted.
func (d *Definition) LauncherNames() []string {
	return sortedKeys(d.Launchers)
}

func newLoadedDefinition(source, path string, def *Definition) (*loadedDefinition, error) {
	loaded := &loadedDefinition{source: source, path: path, def: def}
	for _, launcher := range def.LauncherNames() {
		backend, err := NewDeclarativeBackend(def, launcher, os.Getenv)
		if err != nil {
			return nil, err
		}
		loaded.backends = append(loaded.backends, backend)
	}
	return loaded, nil
}

// launcherConflict reports a launcher name of candidate already provided by a
// Go backend or by another definition (a same-name definition is replaced,
// not a conflict).
func launcherConflict(candidate *loadedDefinition, others []*loadedDefinition) string {
	for _, launcher := range candidate.def.LauncherNames() {
		if launcher == Auto {
			return fmt.Sprintf("launcher %s is reserved", launcher)
		}
		if _, ok := lookupGoBackend(launcher); ok {
			return fmt.Sprintf("launcher %s is a built-in Go launcher", launcher)
		}
		for _, other := range others {
			if other.def.Name == candidate.def.Name {
				continue
			}
			if _, ok := other.def.Launchers[launcher]; ok {
				return fmt.Sprintf("launcher %s is already provided by definition %s", launcher, other.def.Name)
			}
		}
	}
	return ""
}

// ReloadDefinitions discards the loaded user definitions; the next lookup
// reads the share directories again.
func ReloadDefinitions() {
	definitionRegistry.Lock()
	defer definitionRegistry.Unlock()
	definitionRegistry.userLoaded = false
	definitionRegistry.active = nil
	definitionRegistry.reports = nil
}

// DefinitionReports returns the load outcome of every user definition file.
func DefinitionReports() []DefinitionReport {
	definitionRegistry.Lock()
	defer definitionRegistry.Unlock()
	ensureDefinitionsLocked()
	return append([]DefinitionReport{}, definitionRegistry.reports...)
}

// DefinitionInventory includes embedded and user load outcomes in precedence
// order. Invalid overrides remain visible alongside the active fallback.
func DefinitionInventory() []DefinitionReport {
	definitionRegistry.Lock()
	defer definitionRegistry.Unlock()
	ensureDefinitionsLocked()
	var reports []DefinitionReport
	for _, embedded := range definitionRegistry.embedded {
		active := false
		for _, current := range definitionRegistry.active {
			if current == embedded {
				active = true
			}
		}
		reports = append(reports, DefinitionReport{Source: embedded.source, Path: embedded.path,
			Name: embedded.def.Name, Launchers: embedded.def.LauncherNames(), Active: active})
	}
	return append(reports, definitionRegistry.reports...)
}

func activeDefinitions() []*loadedDefinition {
	definitionRegistry.Lock()
	defer definitionRegistry.Unlock()
	ensureDefinitionsLocked()
	return definitionRegistry.active
}

func definitionLauncherNames() []string {
	var names []string
	for _, loaded := range activeDefinitions() {
		names = append(names, loaded.def.LauncherNames()...)
	}
	return names
}

// ensureDefinitionsLocked reads the user definition directories once. A file
// replaces the same-name definition of a lower-precedence source as a whole;
// an invalid file is reported and leaves the lower-precedence definition in
// force.
func ensureDefinitionsLocked() {
	if definitionRegistry.userLoaded {
		return
	}
	active := append([]*loadedDefinition{}, definitionRegistry.embedded...)
	var reports []DefinitionReport
	for _, dir := range DefinitionDirs() {
		for _, file := range readDefinitionDir(dir, &reports) {
			report := DefinitionReport{Source: dir.Source, Path: file.path}
			def, err := DecodeDefinition(file.path, file.data)
			var loaded *loadedDefinition
			if err == nil {
				report.Name, report.Launchers = def.Name, def.LauncherNames()
				loaded, err = newLoadedDefinition(dir.Source, file.path, def)
			}
			if err == nil {
				if conflict := launcherConflict(loaded, active); conflict != "" {
					err = fmt.Errorf("%s: %s", file.path, conflict)
				}
			}
			if err != nil {
				report.Err = err
				reports = append(reports, report)
				continue
			}
			for index := range reports {
				if reports[index].Active && reports[index].Name == def.Name {
					reports[index].Active = false
				}
			}
			report.Active = true
			reports = append(reports, report)
			active = replaceDefinition(active, loaded)
		}
	}
	definitionRegistry.active = active
	definitionRegistry.reports = reports
	definitionRegistry.userLoaded = true
}

func replaceDefinition(active []*loadedDefinition, loaded *loadedDefinition) []*loadedDefinition {
	for index, existing := range active {
		if existing.def.Name == loaded.def.Name {
			out := append([]*loadedDefinition{}, active...)
			out[index] = loaded
			return out
		}
	}
	return append(active, loaded)
}

type definitionFile struct {
	path string
	data []byte
}

// readDefinitionDir reads the *.json regular files of one directory in name
// order without following links. A missing directory has no definitions.
func readDefinitionDir(dir DefinitionDir, reports *[]DefinitionReport) []definitionFile {
	if _, err := os.Lstat(dir.Path); os.IsNotExist(err) {
		return nil
	}
	entries, err := fs.ListDirectory(dir.Root, dir.Path)
	if err != nil {
		*reports = append(*reports, DefinitionReport{Source: dir.Source, Path: dir.Path, Err: err})
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	var files []definitionFile
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		path := filepath.Join(dir.Path, entry.Name)
		if entry.Kind != fs.KindFile {
			*reports = append(*reports, DefinitionReport{Source: dir.Source, Path: path, Err: fmt.Errorf("%s: not a regular file", path)})
			continue
		}
		data, err := fs.ReadRegularFile(dir.Root, path)
		if err != nil {
			*reports = append(*reports, DefinitionReport{Source: dir.Source, Path: path, Err: err})
			continue
		}
		files = append(files, definitionFile{path: path, data: data})
	}
	return files
}

func definitionBackends() []Backend {
	var out []Backend
	for _, loaded := range activeDefinitions() {
		for _, backend := range loaded.backends {
			out = append(out, backend)
		}
	}
	return out
}
