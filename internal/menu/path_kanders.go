package menu

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
	"github.com/dualface/kander/internal/version"
)

// Seams for tests: the inventory, deletion, brew upgrade, and the choice prompt
// are all replaceable so doctor's cleanup flow never touches a real PATH.
var (
	pathKanderBinaries = install.PathKanderBinaries
	removeKanderBinary = install.RemoveKanderBinary
	runBrewUpgrade     = execBrewUpgrade
	askKanderChoice    = askChoice
)

// execBrewUpgrade runs `brew upgrade kander` attached to the terminal.
func execBrewUpgrade() error {
	cmd := exec.Command("brew", "upgrade", "kander")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// reportOutdatedRunningKander is doctor's first check: when any kander on PATH
// reports a strictly newer version than the running binary, doctor stops before
// any check, prompt, or repair, because repairing would write this older
// binary's embedded rules and config schema over newer on-disk copies. An
// unparseable running version (a dev build) and binaries that report no
// version do not trigger the stop.
func reportOutdatedRunningKander(binaries []install.PathKanderBinary) bool {
	running := version.String()
	if !version.Valid(running) {
		return true
	}
	healthy := true
	for _, bin := range binaries {
		if version.Compare(bin.Version, running) > 0 {
			healthy = false
			warning(config.Text(
				"menu.running_kander_is_older_than_path_binary_doctor_stopped", running, bin.Path, bin.Version,
			))
		}
	}
	return healthy
}

// reportPathKanders lists every distinct kander executable reachable through PATH.
// With several installs it flags outdated Homebrew copies and, when interactive,
// offers to run `brew upgrade kander` and to keep one executable while removing
// the other non-Homebrew files. Non-interactive callers get the passive listing.
// The caller supplies the inventory already probed for the version guard so the
// PATH binaries are not version-probed twice per run.
func reportPathKanders(interactive bool, binaries []install.PathKanderBinary) bool {
	if len(binaries) <= 1 {
		return true
	}
	hint(config.Text("menu.found_kander_executables_on_path", fmt.Sprint(len(binaries))))
	latest := 0
	for i := range binaries {
		if version.Compare(binaries[i].Version, binaries[latest].Version) > 0 {
			latest = i
		}
	}
	brewOutdated := false
	nonBrewCount := 0
	for i, bin := range binaries {
		versionText := bin.Version
		if versionText == "" {
			versionText = config.Text("menu.kander_version_unknown")
		}
		line := bin.Path + ": " + versionText
		if bin.Brew {
			line += " " + config.Text("menu.kander_homebrew")
		} else {
			nonBrewCount++
		}
		if i == latest {
			line += " " + config.Text("menu.kander_latest")
		}
		hint(line)
		if bin.Brew && i != latest && version.Compare(bin.Version, binaries[latest].Version) < 0 {
			brewOutdated = true
			warning(config.Text(
				"menu.brew_kander_is_not_the_latest_upgrade_via_brew", bin.Path, binaries[latest].Version,
			))
		}
	}
	if brewOutdated && interactive {
		hint("brew upgrade kander")
		selected, err := askKanderChoice(config.Text("menu.upgrade_kander_via_brew"), []choice{
			{Value: "upgrade", Label: config.Text("menu.upgrade_kander_via_brew")},
			{Value: "skip", Label: config.Text("menu.skip_and_continue_checking")},
		}, "skip")
		if err == nil && selected == "upgrade" {
			hint(config.Text("menu.about_to_run") + "brew upgrade kander")
			if err := runBrewUpgrade(); err != nil {
				warning(config.Text("menu.brew_upgrade_failed_continuing", err.Error()))
			} else {
				success(config.Text("menu.brew_upgrade_completed"))
				// The inventory printed above is stale now; the keep-one question
				// must offer the post-upgrade binaries, not the pre-upgrade ones.
				binaries = pathKanderBinaries()
				nonBrewCount = 0
				for _, bin := range binaries {
					if !bin.Brew {
						nonBrewCount++
					}
				}
			}
		}
	}
	if nonBrewCount == 0 || len(binaries) < 2 {
		return false
	}
	if !interactive {
		hint(config.Text("menu.multiple_kander_run_kander_doctor_in_a_terminal"))
		return false
	}
	choices := make([]choice, 0, len(binaries)+1)
	for _, bin := range binaries {
		label := bin.Path
		if bin.Brew {
			label += " " + config.Text("menu.kander_homebrew")
		}
		choices = append(choices, choice{Value: bin.Path, Label: label})
	}
	choices = append(choices, choice{Value: "", Label: config.Text("menu.skip_and_continue_checking")})
	keep, err := askKanderChoice(config.Text("menu.keep_which_kander_executable"), choices, "")
	if err != nil || keep == "" {
		return false
	}
	brewSkipped := false
	for _, bin := range binaries {
		if bin.Path == keep {
			continue
		}
		if bin.Brew {
			brewSkipped = true
			continue
		}
		if err := removeKanderBinary(bin.Path); err != nil {
			warning(config.Text("menu.kander_executable_remove_failed", bin.Path, err.Error()))
		} else {
			success(config.Text("menu.kander_executable_removed", bin.Path))
		}
	}
	if brewSkipped {
		hint(config.Text("menu.brew_managed_executables_left_alone_uninstall_via_brew"))
	}
	// Report the post-cleanup inventory so a successful keep-one reads healthy.
	return len(pathKanderBinaries()) <= 1
}
