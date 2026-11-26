package servicectl

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/dualface/kander/v2/internal/fs"
)

// The server log and the files a trim keeps beside it in the data directory.
const (
	LogName        = "server.log"
	LogArchiveName = "server.log.1.gz"
	logArchiveTemp = "server.log.1.gz.tmp"
	logTailName    = "server.log.tail"
	logTailTemp    = "server.log.tail.tmp"
)

// LogTrim describes one trim of server.log. Size is the size of the log
// before the trim; Bytes is the head moved into the archive (zero when the log
// was not larger than the limit). Recovered reports that the tail of an
// interrupted earlier trim was appended back first.
type LogTrim struct {
	Path      string
	Size      int64
	Bytes     int64
	Recovered bool
}

// logTrimMu keeps two trims of one process from interleaving: an online gc
// and an automatic trim of the same server.
var logTrimMu sync.Mutex

// logTrimStep, when set by a test, may stop a trim after the named step:
// "archive", "tail" or "truncate".
var logTrimStep func(step string) error

// lineFlush bounds the bytes appendLines buffers before it writes whole lines.
const lineFlush = 1 << 20

// TrimLog keeps the last keep bytes of <dataDir>/server.log, starting at the
// next line start, and moves everything before into server.log.1.gz, which
// keeps one generation. The server holds the log in append mode, so the trim
// copies the tail aside, truncates the log and appends the tail back; the
// server's next write lands after it. A missing log, or one not larger than
// keep, is left alone. A dry run only measures and changes no file.
//
// Lines the server writes between the end of the tail copy and the truncation
// are lost; lines it writes between the truncation and the end of the append
// come before the kept tail. A trim interrupted after its tail copy is
// finished by the next trim, which appends the saved tail back first: the tail
// is never lost, but it may then follow newer lines or appear twice.
func TrimLog(dataDir string, keep int64, dryRun bool) (result LogTrim, err error) {
	if keep <= 0 {
		return result, errors.New("log trim needs a positive size to keep")
	}
	logTrimMu.Lock()
	defer logTrimMu.Unlock()
	root := filepath.Dir(dataDir)
	path := func(name string) string { return filepath.Join(dataDir, name) }
	result.Path = path(LogName)
	if !dryRun {
		if result.Recovered, err = recoverLogTail(root, dataDir); err != nil {
			return result, err
		}
	}
	log, err := fs.OpenRegularFileIfExists(root, result.Path)
	if err != nil || log == nil {
		return result, err
	}
	defer log.Close()
	info, err := log.Stat()
	if err != nil {
		return result, err
	}
	result.Size = info.Size()
	if result.Size <= keep {
		return result, nil
	}
	cut, err := lineStart(log, result.Size-keep, result.Size)
	if err != nil {
		return result, err
	}
	result.Bytes = cut
	if dryRun || cut == 0 {
		return result, nil
	}
	if err := writeLogArchive(root, dataDir, log, cut); err != nil {
		return result, err
	}
	if err := stopAfter("archive"); err != nil {
		return result, err
	}
	if err := saveLogTail(root, dataDir, log, cut); err != nil {
		return result, err
	}
	if err := stopAfter("tail"); err != nil {
		return result, err
	}
	writable, err := fs.OpenWritableRegularFile(root, result.Path)
	if err != nil {
		return result, err
	}
	err = errors.Join(writable.Truncate(0), writable.Close())
	if err != nil {
		return result, err
	}
	if err := stopAfter("truncate"); err != nil {
		return result, err
	}
	return result, appendLogTail(root, dataDir)
}

func stopAfter(step string) error {
	if logTrimStep == nil {
		return nil
	}
	return logTrimStep(step)
}

// lineStart returns the first line start at or after from: from itself when
// the byte before it ends a line, else the byte after the next newline, or end
// when no newline follows.
func lineStart(log *os.File, from, end int64) (int64, error) {
	if from == 0 {
		return 0, nil
	}
	r := bufio.NewReaderSize(io.NewSectionReader(log, from-1, end-from+1), 64<<10)
	offset := from - 1
	for {
		chunk, err := r.ReadSlice('\n')
		offset += int64(len(chunk))
		if err == nil {
			return offset, nil
		}
		if errors.Is(err, io.EOF) {
			return end, nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return 0, err
		}
	}
}

// writeLogArchive compresses [0, cut) of the log into a temporary file and
// then replaces the archive with it.
func writeLogArchive(root, dataDir string, log *os.File, cut int64) (err error) {
	temp := filepath.Join(dataDir, logArchiveTemp)
	if _, err := fs.RemoveRegularFileIfExists(root, temp); err != nil {
		return err
	}
	file, err := fs.CreatePrivateRegularFile(root, temp)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_, removeErr := fs.RemoveRegularFileIfExists(root, temp)
			err = errors.Join(err, removeErr)
		}
	}()
	archive := gzip.NewWriter(file)
	_, err = io.Copy(archive, io.NewSectionReader(log, 0, cut))
	err = errors.Join(err, archive.Close())
	if err == nil {
		err = file.Sync()
	}
	if err = errors.Join(err, file.Close()); err != nil {
		return err
	}
	target := filepath.Join(dataDir, LogArchiveName)
	if _, err = fs.RemoveRegularFileIfExists(root, target); err != nil {
		return err
	}
	return fs.Rename(root, temp, target)
}

// saveLogTail copies the log from cut to its current end into server.log.tail.
// The rename marks a complete copy: a trim interrupted before it never touched
// the log, so its partial copy is simply removed.
func saveLogTail(root, dataDir string, log *os.File, cut int64) (err error) {
	temp := filepath.Join(dataDir, logTailTemp)
	if _, err := fs.RemoveRegularFileIfExists(root, temp); err != nil {
		return err
	}
	file, err := fs.CreatePrivateRegularFile(root, temp)
	if err != nil {
		return err
	}
	if _, err = log.Seek(cut, io.SeekStart); err == nil {
		_, err = io.Copy(file, log)
	}
	if err == nil {
		err = file.Sync()
	}
	if err = errors.Join(err, file.Close()); err != nil {
		_, removeErr := fs.RemoveRegularFileIfExists(root, temp)
		return errors.Join(err, removeErr)
	}
	return fs.Rename(root, temp, filepath.Join(dataDir, logTailName))
}

// recoverLogTail finishes an interrupted trim: a partial tail copy is removed,
// a complete one is appended back to the log.
func recoverLogTail(root, dataDir string) (bool, error) {
	if _, err := fs.RemoveRegularFileIfExists(root, filepath.Join(dataDir, logTailTemp)); err != nil {
		return false, err
	}
	saved, err := fs.RegularFileExists(root, filepath.Join(dataDir, logTailName))
	if err != nil || !saved {
		return false, err
	}
	return true, appendLogTail(root, dataDir)
}

// appendLogTail appends server.log.tail to the log in whole lines, so a line
// the server writes meanwhile never splits a kept line, and then removes it.
func appendLogTail(root, dataDir string) error {
	tailPath := filepath.Join(dataDir, logTailName)
	tail, err := fs.OpenRegularFileIfExists(root, tailPath)
	if err != nil {
		return err
	}
	if tail == nil {
		return errors.New("saved log tail disappeared: " + tailPath)
	}
	defer tail.Close()
	log, err := fs.OpenAppendFile(dataDir, filepath.Join(dataDir, LogName))
	if err != nil {
		return err
	}
	err = appendLines(log, tail)
	if err == nil {
		err = log.Sync()
	}
	if err = errors.Join(err, log.Close()); err != nil {
		return err
	}
	_, err = fs.RemoveRegularFileIfExists(root, tailPath)
	return err
}

// appendLines copies src to dst in writes of whole lines of at most about
// lineFlush bytes. A line longer than lineFlush is written in pieces.
func appendLines(dst io.Writer, src io.Reader) error {
	r := bufio.NewReaderSize(src, 64<<10)
	buffer := make([]byte, 0, lineFlush+64<<10)
	write := func(end int) error {
		_, err := dst.Write(buffer[:end])
		buffer = buffer[:copy(buffer, buffer[end:])]
		return err
	}
	for {
		chunk, err := r.ReadSlice('\n')
		buffer = append(buffer, chunk...)
		if len(buffer) > lineFlush {
			end := bytes.LastIndexByte(buffer, '\n') + 1
			if end == 0 {
				end = len(buffer)
			}
			if writeErr := write(end); writeErr != nil {
				return writeErr
			}
		}
		switch {
		case err == nil, errors.Is(err, bufio.ErrBufferFull):
		case errors.Is(err, io.EOF):
			if len(buffer) == 0 {
				return nil
			}
			return write(len(buffer))
		default:
			return err
		}
	}
}
