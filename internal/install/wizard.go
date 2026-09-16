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

func supportedLanguage(lang string) bool {
	for _, item := range config.Languages {
		if item == lang {
			return true
		}
	}
	return false
}

func runWizard() (Request, error) {
	req := Request{
		Language: config.ResolveLanguage(),
		Mode:     config.ModeGlobal,
	}
	if !supportedLanguage(req.Language) {
		req.Language = "en"
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
				huh.NewOption(config.Text("install.language_ja"), "ja"),
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
	if result.DestBinary == "" {
		fmt.Println(config.Text("install.existing_entry", result.RunBinary))
	}
	if result.Paths.Mode == config.ModeProject {
		fmt.Println(result.DestBinary)
		fmt.Fprintln(os.Stderr, config.Text("install.project_finished"))
	}
	if result.LegacyRemoved {
		fmt.Fprintln(os.Stderr, config.Text("install.legacy_removed"))
	} else if len(result.Legacy) > 0 {
		fmt.Fprintln(os.Stderr, config.Text("install.legacy_kept"))
	}
	for _, path := range result.LegacyLinksRemoved {
		fmt.Fprintln(os.Stderr, config.Text("install.legacy_link_replaced", path))
	}
	for _, item := range result.Integrations {
		target := item.Target
		if target == "" {
			target = item.Agent
		}
		if item.Err != nil {
			fmt.Fprintln(os.Stderr, config.Text("install.rules_reference_failed", target, item.Err.Error()))
			continue
		}
		switch item.Status {
		case IntegrationRewritten:
			fmt.Fprintln(os.Stderr, config.Text("install.rules_reference_updated", target))
		case IntegrationCreated, IntegrationUpdated:
			fmt.Fprintln(os.Stderr, config.Text("install.rules_reference_added", target))
		}
	}
}

// RunInteractive initializes the chosen scope and hands off to its selected entry.
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
		applyGlobalInstallDefaults(&req)
	}
	result, err := Perform(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return finishSuccessfulInstall(result, req.Language)
}

// finishSuccessfulInstall continues with the existing or explicitly installed binary.
func finishSuccessfulInstall(result Result, lang string) int {
	printResult(result)
	if result.Paths.Mode != config.ModeProject && result.DestBinary != "" {
		warnPath(result.RunBinary)
	}
	if err := launchInstalled(result.RunBinary, lang); err != nil {
		fmt.Fprintln(os.Stderr, config.Text("install.failed_handoff", err.Error()))
		return 1
	}
	return 0
}
