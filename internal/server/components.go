package server

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dualface/kander/internal/install"
)

func runComponentInstall(args []string) int {
	flags := flag.NewFlagSet("kander-server install", flag.ContinueOnError)
	dir := flags.String("dir", "", "binary destination directory (default: user install bin)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: kander-server install [--dir DIR]")
		return 2
	}
	path, copied, err := install.InstallServer("", *dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		Path   string `json:"path"`
		Copied bool   `json:"copied"`
	}{path, copied}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runComponentUpdate(args []string) int {
	flags := flag.NewFlagSet("kander-server update", flag.ContinueOnError)
	target := flags.String("version", "", "explicit newer release version, including prereleases; default: latest stable")
	check := flags.Bool("check", false, "print a stable update offer without installing")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *check && *target != "" {
		fmt.Fprintln(os.Stderr, "usage: kander-server update [--check | --version VERSION]")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if *target == "" {
		info, err := install.CheckServerUpdate(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if *check || info == nil {
			if err := json.NewEncoder(os.Stdout).Encode(info); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			return 0
		}
		*target = info.Version
	}
	result, err := install.ApplyServerUpdate(ctx, *target)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
