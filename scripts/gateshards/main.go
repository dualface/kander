// Command gateshards builds and runs the shard plan of release-gate.yml.
//
//	gateshards generate -artifacts DIR -source TEXT [-target SECONDS] [-out FILE]
//	    build a plan from the go test -json artifacts of one gate run
//	    (gh run download <run> -D DIR)
//	gateshards matrix [-plan FILE]
//	    print the native job matrix of the plan as JSON, for fromJSON
//	gateshards run -platform NAME -shard NAME -universe FILE -json-dir DIR [-plan FILE]
//	    run the go test calls of one shard through scripts/go-test-json.sh
//	gateshards compare -old DIR -new DIR
//	    compare the tests two gate runs ran, per platform and suite
//
// Run it from the repository root: go run ./scripts/gateshards <command>.
// docs/release-gate.md describes the plan and when to regenerate it.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const defaultPlan = ".github/release-gate-shards.json"

// defaultTarget is the shard estimate the generator packs to, in seconds. With
// the setup steps of a job it keeps a shard within the 15-minute gate.
const defaultTarget = 540

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "gateshards:", err)
		var usage usageError
		if errors.As(err, &usage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

type usageError struct{ error }

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError{fmt.Errorf("usage: gateshards generate|matrix|run|compare [flags]")}
	}
	flags := flag.NewFlagSet("gateshards "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	planPath := flags.String("plan", defaultPlan, "the shard plan")
	switch args[0] {
	case "generate":
		artifacts := flags.String("artifacts", "", "directory written by gh run download")
		source := flags.String("source", "", "where the timings come from, for generated_from")
		target := flags.Float64("target", defaultTarget, "shard estimate to pack to, in seconds")
		out := flags.String("out", defaultPlan, "file to write the plan to")
		if err := flags.Parse(args[1:]); err != nil {
			return usageError{err}
		}
		if *artifacts == "" || *source == "" {
			return usageError{fmt.Errorf("generate needs -artifacts and -source")}
		}
		module, err := goModule()
		if err != nil {
			return err
		}
		plan, err := Generate(module, *artifacts, *source, DefaultConfigs, *target)
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(*out, append(data, '\n'), 0o644)
	case "matrix":
		if err := flags.Parse(args[1:]); err != nil {
			return usageError{err}
		}
		plan, err := LoadPlan(*planPath)
		if err != nil {
			return err
		}
		data, err := json.Marshal(Matrix(plan))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, string(data))
		return err
	case "run":
		platformName := flags.String("platform", "", "platform name")
		shardName := flags.String("shard", "", "shard name")
		universePath := flags.String("universe", "", "file with the platform's packages, one import path per line")
		jsonDir := flags.String("json-dir", "", "directory for the go test -json files")
		if err := flags.Parse(args[1:]); err != nil {
			return usageError{err}
		}
		if *platformName == "" || *shardName == "" || *universePath == "" || *jsonDir == "" {
			return usageError{fmt.Errorf("run needs -platform, -shard, -universe and -json-dir")}
		}
		plan, err := LoadPlan(*planPath)
		if err != nil {
			return err
		}
		platform, err := plan.Platform(*platformName)
		if err != nil {
			return err
		}
		shard, err := platform.Shard(*shardName)
		if err != nil {
			return err
		}
		module, err := goModule()
		if err != nil {
			return err
		}
		universe, err := ReadUniverse(module, *universePath)
		if err != nil {
			return err
		}
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		return RunShard(root, *jsonDir, platform.Name, shard.Name, Calls(platform, shard, universe), stdout, stderr)
	case "compare":
		oldDir := flags.String("old", "", "artifacts of the earlier run")
		newDir := flags.String("new", "", "artifacts of the later run")
		if err := flags.Parse(args[1:]); err != nil {
			return usageError{err}
		}
		if *oldDir == "" || *newDir == "" {
			return usageError{fmt.Errorf("compare needs -old and -new")}
		}
		oldSets, err := TestSets(*oldDir)
		if err != nil {
			return err
		}
		newSets, err := TestSets(*newDir)
		if err != nil {
			return err
		}
		missing := 0
		for _, diff := range Compare(oldSets, newSets) {
			fmt.Fprintf(stdout, "%s: old %d, new %d, missing %d, added %d\n",
				diff.Group, len(oldSets[diff.Group]), len(newSets[diff.Group]), len(diff.Missing), len(diff.Added))
			for _, test := range diff.Missing {
				fmt.Fprintf(stdout, "  missing %s\n", test)
			}
			for _, test := range diff.Added {
				fmt.Fprintf(stdout, "  added %s\n", test)
			}
			missing += len(diff.Missing)
		}
		if missing > 0 {
			return fmt.Errorf("the new run misses %d tests of the old run", missing)
		}
		return nil
	default:
		return usageError{fmt.Errorf("unknown command %q", args[0])}
	}
}

// MatrixEntry is one job of the native job matrix.
type MatrixEntry struct {
	Platform string `json:"platform"`
	OS       string `json:"os"`
	Shard    string `json:"shard"`
	Common   bool   `json:"common"`
}

// Matrix is the include list of the native job.
func Matrix(plan *Plan) []MatrixEntry {
	var entries []MatrixEntry
	for _, platform := range plan.Platforms {
		for _, shard := range platform.Shards {
			entries = append(entries, MatrixEntry{Platform: platform.Name, OS: platform.OS, Shard: shard.Name, Common: shard.Common})
		}
	}
	return entries
}

func goModule() (string, error) {
	out, err := exec.Command("go", "list", "-m").Output()
	if err != nil {
		return "", fmt.Errorf("go list -m: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
