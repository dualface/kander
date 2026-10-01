//go:build !linux

package process

import "context"

func ObserveOpenFiles(ctx context.Context, pid int) (ProcessFiles, error) {
	if err := ctx.Err(); err != nil {
		return ProcessFiles{}, err
	}
	return ProcessFiles{}, ErrOpenFilesUnsupported
}
