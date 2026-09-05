package fs

import (
	"bytes"
	"io"
)

// AppendUniqueLine 在同一固定叶句柄内去重追加一行, 不改既有 ACL/mode.
// 用于 Git exclude 等中性对象. 行匹配按整行精确比较, 不含换行符.
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
