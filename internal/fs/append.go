package fs

import (
	"bytes"
	"io"
)

// AppendUniqueLine appends one deduplicated line through a single pinned leaf handle, leaving existing ACLs/modes untouched.
// It is meant for neutral objects such as the Git exclude. Lines are matched by exact full-line comparison, excluding the newline.
func AppendUniqueLine(root, path, line string) error {
	file, err := OpenAppendFile(root, path)
	if err != nil {
		return err
	}
	defer file.Close()
	lock, err := LockExclusive(file.File)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	if _, err := file.Seek(0, 0); err != nil {
		return wrap("seek", path, err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return wrap("read", path, err)
	}
	normalized := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	for _, existing := range bytes.Split(normalized, []byte("\n")) {
		if string(existing) == line {
			return nil
		}
	}
	addition := line + "\n"
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		addition = "\n" + addition
	}
	if _, err := file.Write([]byte(addition)); err != nil {
		return wrap("write", path, err)
	}
	return nil
}
