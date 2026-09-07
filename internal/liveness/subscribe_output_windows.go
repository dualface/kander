package liveness

import (
	"context"
	"errors"
	"os"
	"runtime"
	"time"

	"golang.org/x/sys/windows"
)

func subscriptionFileWriter(file *os.File) (ContextWriter, func() error, error) {
	// Overlapped handles use Go's poller cancellation; synchronous handles use
	// a pinned thread so cancellation cannot target unrelated I/O.
	if err := file.SetWriteDeadline(time.Time{}); err == nil {
		writer := contextWriteFunc(func(ctx context.Context, p []byte) (int, error) {
			deadline, _ := ctx.Deadline()
			if err := file.SetWriteDeadline(deadline); err != nil {
				return 0, err
			}
			joined := make(chan struct{})
			var cancelErr error
			stop := context.AfterFunc(ctx, func() { defer close(joined); cancelErr = file.SetWriteDeadline(time.Now()) })
			n, err := file.Write(p)
			if !stop() {
				<-joined
			}
			clearErr := file.SetWriteDeadline(time.Time{})
			if ctx.Err() != nil {
				err = errors.Join(ctx.Err(), err)
			}
			return n, errors.Join(err, clearErr, cancelErr)
		})
		return writer, func() error { return nil }, nil
	}
	var consoleMode uint32
	console := windows.GetConsoleMode(windows.Handle(file.Fd()), &consoleMode) == nil
	cancelIO := windows.NewLazySystemDLL("kernel32.dll").NewProc("CancelSynchronousIo")
	if err := cancelIO.Find(); err != nil {
		return nil, nil, err
	}
	writer := contextWriteFunc(func(ctx context.Context, p []byte) (int, error) {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		thread, err := windows.OpenThread(windows.THREAD_TERMINATE, false, windows.GetCurrentThreadId())
		if err != nil {
			return 0, err
		}
		defer windows.CloseHandle(thread)
		done, joined := make(chan struct{}), make(chan struct{})
		var cancelErr error
		go func() {
			defer close(joined)
			select {
			case <-done:
				return
			case <-ctx.Done():
			}
			// Retry closes the race between the context check and entering WriteFile.
			timer := time.NewTicker(5 * time.Millisecond)
			defer timer.Stop()
			for {
				result, _, err := cancelIO.Call(uintptr(thread))
				if result == 0 && !errors.Is(err, windows.ERROR_NOT_FOUND) {
					cancelErr = err
				}
				select {
				case <-done:
					return
				case <-timer.C:
				}
			}
		}()
		var n int
		if err = ctx.Err(); err == nil {
			if console {
				n, err = file.Write(p)
			} else {
				var written uint32
				err = windows.WriteFile(windows.Handle(file.Fd()), p, &written, nil)
				n = int(written)
			}
		}
		close(done)
		<-joined
		runtime.KeepAlive(file)
		if ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err, cancelErr)
		}
		return n, err
	})
	return writer, func() error { return nil }, nil
}
