package check

import "bytes"

func physicalLines(blob []byte) int {
	if len(blob) == 0 {
		return 0
	}
	n := bytes.Count(blob, []byte{'\n'})
	if blob[len(blob)-1] != '\n' {
		n++
	}
	return n
}
