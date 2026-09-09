package review

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/process"
)

func executeReview(ctx reviewContext, abort <-chan os.Signal) int {
	runtimeDir, err := fs.CreatePrivateTempDir(ctx.tempRoot, ctx.agent+"-review.")
	if err != nil {
		userError(config.Text(
			"review.could_not_create_the_private_review_runtime", err.Error(),
		))
		if ctx.archive != nil {
			ctx.archive.reason = err.Error()
			return ctx.archive.finish(1)
		}
		return 1
	}
	code := executeInRuntime(ctx, runtimeDir.Path, abort)
	if cleanup := runtimeDir.Close(); cleanup != nil {
		userError(config.Text(
			"review.could_not_safely_clean_the_private_review_runtime", cleanup.Error(),
		))
		if ctx.archive != nil {
			ctx.archive.reason = cleanup.Error()
			return ctx.archive.finish(2)
		}
		return 2
	}
	if ctx.archive != nil {
		return ctx.archive.finish(code)
	}
	return code
}

func executeInRuntime(ctx reviewContext, runtime string, abort <-chan os.Signal) (exitCode int) {
	outputRoot := runtime
	outputName := ctx.settings.outputName
	if ctx.archive != nil {
		outputRoot = ctx.archive.staging
		outputName = "output.raw"
	}
	outputFile := filepath.Join(outputRoot, outputName)
	stdoutFile := filepath.Join(outputRoot, "stdout.log")
	errorFile := filepath.Join(outputRoot, "error.log")
	evidenceFile := filepath.Join(runtime, "evidence.txt")
	promptFile := filepath.Join(runtime, "prompt.txt")
	stdinFile := filepath.Join(runtime, "stdin.txt")

	var lp *launchedProcess
	reviewStarted := false
	treeAttempted := false
	treeCollected := false
	var failure *gateError
	timedOut := false
	outputPrinted := false

	fail := func(err error) {
		var ge *gateError
		if errors.As(err, &ge) {
			failure = ge
			exitCode = ge.code
			return
		}
		failure = newGate(2, "review.review_runtime_i_o_failed", runtime, err.Error())
		exitCode = 2
	}

	defer func() {
		if lp != nil && !treeAttempted {
			treeAttempted = true
			if _, err := stopProcessTree(lp); err != nil {
				var ge *gateError
				if errors.As(err, &ge) {
					failure = ge
					exitCode = ge.code
				}
			} else {
				treeCollected = true
			}
		}
		if failure != nil && failure.message != "" {
			if ctx.archive != nil {
				ctx.archive.reason = failure.message
			}
			userError(failure.message)
		}
		if reviewStarted && !treeCollected {
			if ctx.archive != nil {
				ctx.archive.reason = config.Text("review.archive_tree_incomplete")
			}
			exitCode = 2
		} else if reviewStarted && !targetIsUnchanged(ctx) {
			if ctx.archive != nil {
				ctx.archive.reason = config.Text("review.review_modified_the_target_worktree", ctx.settings.name, ctx.root)
			}
			userError(config.Text(
				"review.review_modified_the_target_worktree", ctx.settings.name, ctx.root,
			))
			exitCode = 2
		}
		if ctx.archive != nil && exitCode != 0 && !outputPrinted {
			printFile(outputRoot, outputFile, os.Stdout)
		}
	}()

	taskContext := ctx.taskContext
	if (ctx.archive != nil || ctx.settings.snapshotSpec) && ctx.taskSpec != "" {
		snapshot := filepath.Join(runtime, "task-spec.md")
		var snapshotErr error
		if ctx.archive != nil {
			snapshotErr = fs.WriteTextAtomic(runtime, snapshot, string(ctx.archive.taskSnapshot), false)
			if snapshotErr == nil {
				snapshotErr = fs.MakeRegularFileReadOnly(runtime, snapshot)
			}
		} else {
			snapshotErr = copySpecSnapshot(ctx.taskSpec, runtime, snapshot)
		}
		if snapshotErr != nil {
			fail(newGate(2,
				"review.could_not_snapshot_spec_file_for", ctx.settings.name, ctx.taskSpec,
			))
			return exitCode
		}
		taskContext = "Authoritative spec file: " + snapshot + ". Read it completely before reviewing."
	}
	if err := writeEvidence(ctx, runtime, evidenceFile); err != nil {
		fail(err)
		return exitCode
	}
	promptBody := buildPrompt(ctx, evidenceFile, taskContext)
	if err := process.WriteTaskFile(runtime, promptFile, promptBody); err != nil {
		fail(err)
		return exitCode
	}
	promptOnDisk, readPromptErr := fs.ReadRegularFile(runtime, promptFile)
	if readPromptErr != nil {
		fail(readPromptErr)
		return exitCode
	}
	ctx.instruction = process.TaskFileInstruction("Perform the "+ctx.role+" review.", promptFile)
	if ctx.settings.stdin == config.ReviewStdinInstruction {
		if err := fs.WriteTextAtomic(runtime, stdinFile, ctx.instruction+"\n", false); err != nil {
			fail(err)
			return exitCode
		}
	}
	ctx.promptFilePaths = map[string]string{}
	for _, file := range ctx.settings.promptFiles {
		rendered, expErr := process.ExpandTemplate(file.Template, map[string]string{
			"inspection":      ctx.settings.inspectionRules,
			"prompt":          string(promptOnDisk),
			"role":            ctx.role,
			"report_language": ctx.reportLanguage,
			"root":            ctx.root,
			"runtime":         runtime,
			"output":          outputFile,
		}, config.ReviewPromptFilePlaceholders())
		if expErr != nil {
			fail(newGateMsg(2, expErr.Error()))
			return exitCode
		}
		dest := filepath.Join(runtime, filepath.FromSlash(file.Path))
		if err := fs.WriteTextAtomic(runtime, dest, rendered, false); err != nil {
			fail(err)
			return exitCode
		}
		abs, absErr := filepath.Abs(dest)
		if absErr != nil {
			fail(absErr)
			return exitCode
		}
		ctx.promptFilePaths[file.Name] = abs
	}
	for _, path := range []string{outputFile, stdoutFile, errorFile} {
		if ctx.archive != nil {
			continue
		}
		if err := fs.WriteTextAtomic(outputRoot, path, "", false); err != nil {
			fail(err)
			return exitCode
		}
	}
	inv, processCWD, err := reviewerArguments(ctx, runtime, outputFile, promptFile)
	if err != nil {
		fail(err)
		return exitCode
	}

	var promptStream *os.File
	if ctx.settings.stdin == config.ReviewStdinInstruction {
		promptStream, err = fs.OpenRegularFileIfExists(runtime, stdinFile)
		if err != nil {
			fail(err)
			return exitCode
		}
		defer promptStream.Close()
	} else {
		promptStream, err = os.OpenFile(os.DevNull, os.O_RDONLY, 0)
		if err != nil {
			fail(err)
			return exitCode
		}
		defer promptStream.Close()
	}
	stdoutStream, err := fs.OpenWritableRegularFile(outputRoot, stdoutFile)
	if err != nil {
		fail(err)
		return exitCode
	}
	defer stdoutStream.Close()
	errorStream, err := fs.OpenWritableRegularFile(outputRoot, errorFile)
	if err != nil {
		fail(err)
		return exitCode
	}
	defer errorStream.Close()

	reviewerStdout := stdoutStream
	if ctx.settings.output.Source == process.SourceStdout {
		out, openErr := fs.OpenWritableRegularFile(outputRoot, outputFile)
		if openErr != nil {
			fail(openErr)
			return exitCode
		}
		defer out.Close()
		reviewerStdout = out
	}

	if ctx.archive != nil {
		if err = ctx.archive.snapshotRuntime(runtime); err != nil {
			fail(err)
			return exitCode
		}
		if err = ctx.archive.phase("launching", "unknown"); err != nil {
			fail(err)
			return exitCode
		}
	}
	lp, err = launchProcess(inv, processCWD, promptStream, reviewerStdout, errorStream)
	if err != nil {
		if ctx.archive != nil {
			ctx.archive.run.LaunchStatus = "not_started"
		}
		fail(newGate(127,
			"review.could_not_start_cli", ctx.settings.name, err.Error(),
		))
		return exitCode
	}
	reviewStarted = true
	if ctx.archive != nil {
		if err = ctx.archive.phase("running", "started"); err != nil {
			fail(err)
			return exitCode
		}
	}
	exitCode, timedOut = monitorProcess(ctx, outputRoot, lp, errorFile, abort)
	treeAttempted = true
	lingering, stopErr := stopProcessTree(lp)
	if stopErr != nil {
		fail(stopErr)
		return exitCode
	}
	treeCollected = true
	if exitCode == 0 && lingering {
		fail(newGate(2,
			"review.review_left_background_child_processes_the_result_was_rejected", ctx.settings.name,
		))
		return exitCode
	}
	if timedOut || exitCode != 0 {
		outputPrinted = true
		printFile(outputRoot, errorFile, os.Stderr)
		printFile(outputRoot, stdoutFile, os.Stdout)
		printFile(outputRoot, outputFile, os.Stdout)
		return exitCode
	}
	errorOutput, readErr := fs.ReadRegularFile(outputRoot, errorFile)
	if readErr != nil {
		fail(readErr)
		return exitCode
	}
	if len(errorOutput) > 0 {
		_, _ = os.Stderr.Write(errorOutput)
		syncStream(os.Stderr)
	}
	outputPrinted = true
	if err := parseReviewOutput(ctx, outputRoot, outputFile, stdoutFile); err != nil {
		fail(err)
		return exitCode
	}
	exitCode = 0
	return exitCode
}

func monitorProcess(ctx reviewContext, runtime string, lp *launchedProcess, errorFile string, abort <-chan os.Signal) (int, bool) {
	started := time.Now()
	nextCheck := started.Add(time.Duration(ctx.settings.checkInterval) * time.Second)
	attachTracker(ctx, lp)
	if abort == nil {
		abort = make(chan os.Signal)
	}
	for {
		observeTracker(lp)
		select {
		case <-lp.waitDone:
			observeTracker(lp)
			return waitExitCode(lp.waitErr, lp), false
		case sig := <-abort:
			return signalExitCode(sig), true
		default:
		}
		if time.Since(started) >= time.Duration(ctx.settings.maxRuntime)*time.Second {
			userError(config.Text(
				"review.review_exceeded_seconds", ctx.settings.name, itoa(ctx.settings.maxRuntime),
			))
			return 124, true
		}
		now := time.Now()
		if !now.Before(nextCheck) {
			elapsed := int(now.Sub(started).Seconds())
			msg := config.Text(
				"review.review_is_still_running_after_s", ctx.settings.name, itoa(elapsed),
			)
			_, _ = os.Stderr.WriteString(msg + "\n")
			printErrorTail(runtime, errorFile)
			nextCheck = nextCheck.Add(time.Duration(ctx.settings.checkInterval) * time.Second)
		}
		remaining := time.Duration(ctx.settings.maxRuntime)*time.Second - time.Since(started)
		if remaining < 10*time.Millisecond {
			remaining = 10 * time.Millisecond
		}
		slice := monitorSlice(ctx, remaining)
		timer := time.NewTimer(slice)
		select {
		case <-lp.waitDone:
			timer.Stop()
			observeTracker(lp)
			return waitExitCode(lp.waitErr, lp), false
		case sig := <-abort:
			timer.Stop()
			return signalExitCode(sig), true
		case <-timer.C:
			observeTracker(lp)
		}
	}
}

func signalExitCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 130
}
