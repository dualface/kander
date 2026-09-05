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
		return 1
	}
	code := executeInRuntime(ctx, runtimeDir.Path, abort)
	if cleanup := runtimeDir.Close(); cleanup != nil {
		userError(config.Text(
			"review.could_not_safely_clean_the_private_review_runtime", cleanup.Error(),
		))
		return 2
	}
	return code
}

func executeInRuntime(ctx reviewContext, runtime string, abort <-chan os.Signal) (exitCode int) {
	outputFile := filepath.Join(runtime, ctx.settings.outputName)
	stdoutFile := filepath.Join(runtime, "stdout.log")
	errorFile := filepath.Join(runtime, "error.log")
	evidenceFile := filepath.Join(runtime, "evidence.txt")
	promptFile := filepath.Join(runtime, "prompt.txt")
	stdinFile := filepath.Join(runtime, "stdin.txt")

	var lp *launchedProcess
	reviewStarted := false
	treeAttempted := false
	treeCollected := false
	var failure *gateError
	timedOut := false

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
			userError(failure.message)
		}
		if reviewStarted && !treeCollected {
			exitCode = 2
		} else if reviewStarted && !targetIsUnchanged(ctx) {
			userError(config.Text(
				"review.review_modified_the_target_worktree", ctx.settings.name, ctx.root,
			))
			exitCode = 2
		}
	}()

	taskContext := ctx.taskContext
	if (ctx.agent == "claude" || ctx.agent == "cursor") && ctx.taskSpec != "" {
		snapshot := filepath.Join(runtime, "task-spec.md")
		if err := copySpecSnapshot(ctx.taskSpec, runtime, snapshot); err != nil {
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
	if err := process.WriteTaskFile(runtime, promptFile, buildPrompt(ctx, evidenceFile, taskContext)); err != nil {
		fail(err)
		return exitCode
	}
	instruction := process.TaskFileInstruction("Perform the "+ctx.role+" review.", promptFile) + "\n"
	if err := fs.WriteTextAtomic(runtime, stdinFile, instruction, false); err != nil {
		fail(err)
		return exitCode
	}
	for _, path := range []string{outputFile, stdoutFile, errorFile} {
		if err := fs.WriteTextAtomic(runtime, path, "", false); err != nil {
			fail(err)
			return exitCode
		}
	}
	inv, processCWD, err := reviewerArguments(ctx, runtime, outputFile, promptFile)
	if err != nil {
		fail(err)
		return exitCode
	}

	promptStream, err := fs.OpenRegularFileIfExists(runtime, stdinFile)
	if err != nil {
		fail(err)
		return exitCode
	}
	defer promptStream.Close()
	stdoutStream, err := fs.OpenWritableRegularFile(runtime, stdoutFile)
	if err != nil {
		fail(err)
		return exitCode
	}
	defer stdoutStream.Close()
	errorStream, err := fs.OpenWritableRegularFile(runtime, errorFile)
	if err != nil {
		fail(err)
		return exitCode
	}
	defer errorStream.Close()

	reviewerStdout := stdoutStream
	if ctx.agent != "codex" {
		out, openErr := fs.OpenWritableRegularFile(runtime, outputFile)
		if openErr != nil {
			fail(openErr)
			return exitCode
		}
		defer out.Close()
		reviewerStdout = out
	}

	lp, err = launchProcess(inv, processCWD, promptStream, reviewerStdout, errorStream)
	if err != nil {
		fail(newGate(127,
			"review.could_not_start_cli", ctx.settings.name, err.Error(),
		))
		return exitCode
	}
	reviewStarted = true
	exitCode, timedOut = monitorProcess(ctx, runtime, lp, errorFile, abort)
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
		printFile(runtime, errorFile, os.Stderr)
		printFile(runtime, stdoutFile, os.Stdout)
		printFile(runtime, outputFile, os.Stdout)
		return exitCode
	}
	errorOutput, readErr := fs.ReadRegularFile(runtime, errorFile)
	if readErr != nil {
		fail(readErr)
		return exitCode
	}
	if len(errorOutput) > 0 {
		_, _ = os.Stderr.Write(errorOutput)
		_ = os.Stderr.Sync()
	}
	if err := parseReviewOutput(ctx, runtime, outputFile, stdoutFile); err != nil {
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
