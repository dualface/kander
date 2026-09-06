package install

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"

	"github.com/dualface/kander/internal/config"
)

func isTTY(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func requireInteractive() error {
	if isTTY(os.Stdin) && isTTY(os.Stdout) {
		return nil
	}
	return fmt.Errorf("%s", config.Text("install.requires_terminal"))
}

func applyLanguage(lang string) {
	config.ApplyLanguageArgument([]string{"kander", "--lang", lang})
}

func runWizard() (Request, error) {
	req := Request{
		Language: config.ResolveLanguage(),
		Mode:     config.ModeGlobal,
	}
	if req.Language != "cn" && req.Language != "en" {
		req.Language = "cn"
	}
	cwd, _ := os.Getwd()
	project := cwd
	scope := "global"

	langForm := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(config.Text("install.choose_language")).
			Options(
				huh.NewOption(config.Text("install.language_cn"), "cn"),
				huh.NewOption(config.Text("install.language_en"), "en"),
			).
			Value(&req.Language),
	))
	if err := langForm.Run(); err != nil {
		return req, mapWizardErr(err)
	}
	applyLanguage(req.Language)

	destForm := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(config.Text("install.choose_destination")).
			Options(
				huh.NewOption(config.Text("install.destination_global"), "global"),
				huh.NewOption(config.Text("install.destination_project"), "project"),
			).
			Value(&scope),
	))
	if err := destForm.Run(); err != nil {
		return req, mapWizardErr(err)
	}
	if scope == "project" {
		req.Mode = config.ModeProject
		input := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title(config.Text("install.project_directory")).
				Value(&project),
		))
		if err := input.Run(); err != nil {
			return req, mapWizardErr(err)
		}
		req.Project = project
	}
	return req, nil
}

func confirmDeleteLegacy(names []string) (bool, error) {
	fmt.Fprintln(os.Stderr, config.Text("install.legacy_detected"))
	fmt.Fprintln(os.Stderr, "  "+joinNames(names))
	fmt.Fprintln(os.Stderr, config.Text("install.legacy_unified"))
	delete := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(config.Text("install.delete_legacy")).
			Affirmative(config.Text("install.confirm_yes")).
			Negative(config.Text("install.confirm_no")).
			Value(&delete),
	))
	if err := form.Run(); err != nil {
		return false, mapWizardErr(err)
	}
	return delete, nil
}

func joinNames(names []string) string {
	out := ""
	for i, name := range names {
		if i > 0 {
			out += " "
		}
		out += name
	}
	return out
}

func mapWizardErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, huh.ErrUserAborted) {
		return fmt.Errorf("%s", config.Text("install.wizard_cancelled"))
	}
	return err
}

func printResult(result Result) {
	fmt.Println(config.Text("install.installed"))
	if result.Paths.Mode == config.ModeProject {
		fmt.Println(result.DestBinary)
		fmt.Fprintln(os.Stderr, config.Text("install.project_finished"))
	}
	if result.LegacyRemoved {
		fmt.Fprintln(os.Stderr, config.Text("install.legacy_removed"))
	} else if len(result.Legacy) > 0 {
		fmt.Fprintln(os.Stderr, config.Text("install.legacy_kept"))
	}
}

// RunInteractive runs the install wizard, performs the install, and always hands off to the dest binary.
func RunInteractive() int {
	if err := requireInteractive(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	req, err := runWizard()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if req.Mode != config.ModeProject {
		paths, err := config.GlobalInstallPaths()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		legacy, err := scanLegacy(paths.BinDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if len(legacy) > 0 {
			delete, err := confirmDeleteLegacy(legacy)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			req.DeleteLegacy = delete
		}
	}
	result, err := Perform(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return finishSuccessfulInstall(result, req.Language)
}

// finishSuccessfulInstall prints the install result and always hands off to the installed binary.
func finishSuccessfulInstall(result Result, lang string) int {
	printResult(result)
	if err := launchInstalled(result.DestBinary, lang); err != nil {
		fmt.Fprintln(os.Stderr, config.Text("install.failed_handoff", err.Error()))
		return 1
	}
	return 0
}
