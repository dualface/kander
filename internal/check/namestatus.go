package check

import (
	"bytes"
	"fmt"
)

type change struct {
	letter  byte
	oldPath []byte
	path    []byte
}

func parseNameStatus(data []byte) ([]change, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if data[len(data)-1] != 0 {
		return nil, fmt.Errorf("truncated name-status")
	}
	parts := bytes.Split(data[:len(data)-1], []byte{0})
	var changes []change
	for i := 0; i < len(parts); {
		status := parts[i]
		if len(status) == 0 {
			return nil, fmt.Errorf("empty name-status token")
		}
		i++
		if i >= len(parts) {
			return nil, fmt.Errorf("truncated name-status")
		}
		letter := status[0]
		path1 := bytes.Clone(parts[i])
		i++
		item := change{letter: letter, path: path1}
		if letter == 'R' || letter == 'C' {
			if i >= len(parts) {
				return nil, fmt.Errorf("truncated name-status")
			}
			item.oldPath = path1
			item.path = bytes.Clone(parts[i])
			i++
		}
		if len(item.path) == 0 {
			return nil, fmt.Errorf("empty name-status path")
		}
		changes = append(changes, item)
	}
	return changes, nil
}

func changePaths(item change) [][]byte {
	paths := [][]byte{item.path}
	if item.oldPath != nil && !bytes.Equal(item.oldPath, item.path) {
		paths = append(paths, item.oldPath)
	}
	return paths
}
