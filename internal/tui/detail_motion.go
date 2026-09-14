package tui

import (
	"unicode"
)

const (
	kindEOF detailKind = iota
	kindSpace
	kindWord
	kindPunct
)

type detailKind int

type detailPos struct {
	line int
	col  int
}

func (p detailPos) coords() [2]int {
	return [2]int{p.line, p.col}
}

func posFrom(coords [2]int) detailPos {
	return detailPos{line: coords[0], col: coords[1]}
}

func clampDetailPos(lines []string, p detailPos) detailPos {
	if len(lines) == 0 {
		return detailPos{}
	}
	if p.line < 0 {
		p.line = 0
	}
	if p.line > len(lines)-1 {
		p.line = len(lines) - 1
	}
	n := runeCount(lines[p.line])
	if p.col < 0 {
		p.col = 0
	}
	if p.col > n {
		p.col = n
	}
	return p
}

func compareDetailPos(a, b detailPos) int {
	if a.line != b.line {
		if a.line < b.line {
			return -1
		}
		return 1
	}
	if a.col < b.col {
		return -1
	}
	if a.col > b.col {
		return 1
	}
	return 0
}

func detailRuneAt(lines []string, p detailPos) (rune, bool) {
	if p.line < 0 || p.line >= len(lines) {
		return 0, false
	}
	runes := []rune(lines[p.line])
	if p.col < 0 || p.col >= len(runes) {
		return 0, false
	}
	return runes[p.col], true
}

func kindAt(lines []string, p detailPos) detailKind {
	if p.line < 0 || p.line >= len(lines) {
		return kindEOF
	}
	runes := []rune(lines[p.line])
	if p.col < 0 || p.col > len(runes) {
		return kindEOF
	}
	if p.col == len(runes) {
		return kindSpace
	}
	return runeKind(runes[p.col])
}

func runeKind(r rune) detailKind {
	if unicode.IsSpace(r) {
		return kindSpace
	}
	if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
		return kindWord
	}
	return kindPunct
}

func wordClass(kind detailKind, big bool) detailKind {
	if big && kind == kindPunct {
		return kindWord
	}
	return kind
}

func stepForward(lines []string, p detailPos) (detailPos, bool) {
	if len(lines) == 0 || p.line < 0 || p.line >= len(lines) {
		return p, false
	}
	n := runeCount(lines[p.line])
	if p.col < n {
		return detailPos{line: p.line, col: p.col + 1}, true
	}
	if p.line+1 < len(lines) {
		return detailPos{line: p.line + 1, col: 0}, true
	}
	return p, false
}

func stepBack(lines []string, p detailPos) (detailPos, bool) {
	if len(lines) == 0 || p.line < 0 || p.line >= len(lines) {
		return p, false
	}
	if p.col > 0 {
		return detailPos{line: p.line, col: p.col - 1}, true
	}
	if p.line > 0 {
		prev := p.line - 1
		return detailPos{line: prev, col: runeCount(lines[prev])}, true
	}
	return p, false
}

func skipWhile(lines []string, p detailPos, dir int, keep func(detailKind) bool) detailPos {
	for {
		kind := kindAt(lines, p)
		if kind == kindEOF || !keep(kind) {
			return p
		}
		var ok bool
		next := p
		if dir > 0 {
			next, ok = stepForward(lines, p)
		} else {
			next, ok = stepBack(lines, p)
		}
		if !ok {
			return p
		}
		p = next
	}
}

func atWordEnd(lines []string, p detailPos, big bool) bool {
	kind := wordClass(kindAt(lines, p), big)
	if kind == kindSpace || kind == kindEOF {
		return false
	}
	next, ok := stepForward(lines, p)
	if !ok {
		return true
	}
	return wordClass(kindAt(lines, next), big) != kind
}

func wordForward(lines []string, p detailPos, big bool) detailPos {
	p = clampDetailPos(lines, p)
	start := p
	kind := wordClass(kindAt(lines, p), big)
	if kind != kindSpace && kind != kindEOF {
		same := kind
		p = skipWhile(lines, p, 1, func(k detailKind) bool {
			return wordClass(k, big) == same
		})
	}
	p = skipWhile(lines, p, 1, func(k detailKind) bool {
		return k == kindSpace
	})
	if wordClass(kindAt(lines, p), big) == kindSpace || kindAt(lines, p) == kindEOF {
		return start
	}
	return p
}

func wordBack(lines []string, p detailPos, big bool) detailPos {
	p = clampDetailPos(lines, p)
	prev, ok := stepBack(lines, p)
	if !ok {
		return p
	}
	p = skipWhile(lines, prev, -1, func(k detailKind) bool {
		return k == kindSpace
	})
	kind := wordClass(kindAt(lines, p), big)
	if kind == kindSpace || kind == kindEOF {
		return p
	}
	for {
		back, ok := stepBack(lines, p)
		if !ok {
			return p
		}
		if wordClass(kindAt(lines, back), big) != kind {
			return p
		}
		p = back
	}
}

func wordEnd(lines []string, p detailPos, big bool) detailPos {
	p = clampDetailPos(lines, p)
	start := p
	if kindAt(lines, p) == kindSpace || kindAt(lines, p) == kindEOF || atWordEnd(lines, p, big) {
		next, ok := stepForward(lines, p)
		if !ok {
			return start
		}
		p = skipWhile(lines, next, 1, func(k detailKind) bool {
			return k == kindSpace
		})
		if kindAt(lines, p) == kindSpace || kindAt(lines, p) == kindEOF {
			return start
		}
	}
	kind := wordClass(kindAt(lines, p), big)
	for {
		next, ok := stepForward(lines, p)
		if !ok {
			return p
		}
		if wordClass(kindAt(lines, next), big) != kind {
			return p
		}
		p = next
	}
}

func lineBegin(p detailPos) detailPos {
	return detailPos{line: p.line, col: 0}
}

func lineFirstNonBlank(lines []string, p detailPos) detailPos {
	p = clampDetailPos(lines, p)
	runes := []rune(lines[p.line])
	for i, r := range runes {
		if !unicode.IsSpace(r) {
			return detailPos{line: p.line, col: i}
		}
	}
	return detailPos{line: p.line, col: 0}
}

func lineEnd(lines []string, p detailPos) detailPos {
	p = clampDetailPos(lines, p)
	return detailPos{line: p.line, col: runeCount(lines[p.line])}
}

func isBlankLine(line string) bool {
	runes := []rune(line)
	for _, r := range runes {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func paraForward(lines []string, p detailPos) detailPos {
	p = clampDetailPos(lines, p)
	if len(lines) == 0 {
		return p
	}
	i := p.line
	n := len(lines)
	if !isBlankLine(lines[i]) {
		for i < n && !isBlankLine(lines[i]) {
			i++
		}
	} else {
		for i < n && isBlankLine(lines[i]) {
			i++
		}
		for i < n && !isBlankLine(lines[i]) {
			i++
		}
	}
	if i >= n {
		last := n - 1
		return detailPos{line: last, col: runeCount(lines[last])}
	}
	return detailPos{line: i, col: 0}
}

func paraBack(lines []string, p detailPos) detailPos {
	p = clampDetailPos(lines, p)
	if len(lines) == 0 {
		return p
	}
	i := p.line
	if !isBlankLine(lines[i]) {
		for i > 0 && !isBlankLine(lines[i]) {
			i--
		}
		return detailPos{line: i, col: 0}
	}
	for i > 0 && isBlankLine(lines[i]) {
		i--
	}
	for i > 0 && !isBlankLine(lines[i]) {
		i--
	}
	return detailPos{line: i, col: 0}
}

func matchBracket(lines []string, p detailPos) (detailPos, bool) {
	p = clampDetailPos(lines, p)
	if p.line < 0 || p.line >= len(lines) {
		return p, false
	}
	runes := []rune(lines[p.line])
	idx := -1
	var br rune
	for i := p.col; i < len(runes); i++ {
		if closer, ok := bracketMate(runes[i]); ok {
			idx = i
			br = runes[i]
			_ = closer
			break
		}
	}
	if idx < 0 {
		return p, false
	}
	return scanBracket(lines, detailPos{line: p.line, col: idx}, br)
}

func bracketMate(r rune) (rune, bool) {
	switch r {
	case '(':
		return ')', true
	case '[':
		return ']', true
	case '{':
		return '}', true
	case ')':
		return '(', true
	case ']':
		return '[', true
	case '}':
		return '{', true
	}
	return 0, false
}

func isOpenBracket(r rune) bool {
	return r == '(' || r == '[' || r == '{'
}

func scanBracket(lines []string, start detailPos, br rune) (detailPos, bool) {
	mate, ok := bracketMate(br)
	if !ok {
		return start, false
	}
	dir := 1
	if !isOpenBracket(br) {
		dir = -1
	}
	depth := 1
	p := start
	for {
		var next detailPos
		var stepped bool
		if dir > 0 {
			next, stepped = stepForward(lines, p)
		} else {
			next, stepped = stepBack(lines, p)
		}
		if !stepped {
			return start, false
		}
		p = next
		r, onChar := detailRuneAt(lines, p)
		if !onChar {
			continue
		}
		if r == br {
			depth++
		} else if r == mate {
			depth--
			if depth == 0 {
				return p, true
			}
		}
	}
}

func findChar(lines []string, p detailPos, ch rune, forward, till bool, count int) (detailPos, bool) {
	p = clampDetailPos(lines, p)
	if count < 1 {
		count = 1
	}
	if p.line < 0 || p.line >= len(lines) {
		return p, false
	}
	runes := []rune(lines[p.line])
	found := 0
	if forward {
		for i := p.col + 1; i < len(runes); i++ {
			if runes[i] != ch {
				continue
			}
			found++
			if found < count {
				continue
			}
			dest := i
			if till {
				dest = i - 1
			}
			if dest == p.col || dest < 0 {
				return p, false
			}
			return detailPos{line: p.line, col: dest}, true
		}
	} else {
		for i := p.col - 1; i >= 0; i-- {
			if runes[i] != ch {
				continue
			}
			found++
			if found < count {
				continue
			}
			dest := i
			if till {
				dest = i + 1
			}
			if dest == p.col || dest > len(runes) {
				return p, false
			}
			return detailPos{line: p.line, col: dest}, true
		}
	}
	return p, false
}

func flipFindCmd(cmd string) string {
	switch cmd {
	case "f":
		return "F"
	case "F":
		return "f"
	case "t":
		return "T"
	case "T":
		return "t"
	}
	return cmd
}

func inclusiveMotion(cmd string) bool {
	switch cmd {
	case "e", "E", "f", "t", "$":
		return true
	}
	return false
}

func bumpExclusive(lines []string, p detailPos) detailPos {
	p = clampDetailPos(lines, p)
	n := 0
	if p.line >= 0 && p.line < len(lines) {
		n = runeCount(lines[p.line])
	}
	if p.col < n {
		return detailPos{line: p.line, col: p.col + 1}
	}
	return p
}

func visualExclusiveEnd(lines []string, origin, dest detailPos) detailPos {
	dest = clampDetailPos(lines, dest)
	if compareDetailPos(dest, origin) < 0 {
		return dest
	}
	return bumpExclusive(lines, dest)
}

func wordRange(lines []string, p detailPos, big, around bool, count int) (detailPos, detailPos, bool) {
	p = clampDetailPos(lines, p)
	if count < 1 {
		count = 1
	}
	kind := wordClass(kindAt(lines, p), big)
	start := p
	if kind == kindEOF {
		return p, p, false
	}
	if kind == kindSpace {
		back, ok := stepBack(lines, p)
		for ok && kindAt(lines, back) == kindSpace {
			start = back
			back, ok = stepBack(lines, start)
		}
	} else {
		start = wordBack(lines, detailPos{line: p.line, col: p.col + 1}, big)
		if wordClass(kindAt(lines, start), big) != kind {
			start = p
		}
	}
	end := start
	for i := 0; i < count; i++ {
		curKind := wordClass(kindAt(lines, end), big)
		if curKind == kindEOF {
			break
		}
		end = skipWhile(lines, end, 1, func(k detailKind) bool {
			return wordClass(k, big) == curKind
		})
		if around {
			if curKind != kindSpace {
				end = skipWhile(lines, end, 1, func(k detailKind) bool {
					return k == kindSpace
				})
			} else {
				nextKind := wordClass(kindAt(lines, end), big)
				if nextKind != kindSpace && nextKind != kindEOF {
					end = skipWhile(lines, end, 1, func(k detailKind) bool {
						return wordClass(k, big) == nextKind
					})
				} else {
					lead := start
					back, ok := stepBack(lines, start)
					if ok && wordClass(kindAt(lines, back), big) != kindSpace {
						prevKind := wordClass(kindAt(lines, back), big)
						for ok && wordClass(kindAt(lines, back), big) == prevKind {
							lead = back
							back, ok = stepBack(lines, lead)
						}
						start = lead
					}
				}
			}
		}
		if i < count-1 && kindAt(lines, end) == kindEOF {
			break
		}
	}
	if compareDetailPos(start, end) >= 0 {
		return start, end, false
	}
	return start, end, true
}

func quoteRange(line string, col int, quote rune, around bool, count int) (int, int, bool) {
	runes := []rune(line)
	if col < 0 {
		col = 0
	}
	if col > len(runes) {
		col = len(runes)
	}
	if count < 1 {
		count = 1
	}
	idxs := make([]int, 0, 8)
	for i, r := range runes {
		if r == quote {
			idxs = append(idxs, i)
		}
	}
	if len(idxs) < 2 {
		return 0, 0, false
	}
	pair := -1
	for i := 0; i+1 < len(idxs); i += 2 {
		left, right := idxs[i], idxs[i+1]
		if col >= left && col <= right {
			pair = i
			break
		}
	}
	if pair < 0 {
		for i := 0; i+1 < len(idxs); i += 2 {
			if idxs[i] >= col {
				pair = i
				break
			}
		}
	}
	if pair < 0 {
		pair = (len(idxs)/2 - 1) * 2
		if pair < 0 {
			return 0, 0, false
		}
	}
	last := pair + (count-1)*2
	if last+1 >= len(idxs) {
		last = (len(idxs)/2 - 1) * 2
	}
	left, right := idxs[pair], idxs[last+1]
	if around {
		return left, right + 1, true
	}
	if left+1 >= right {
		return 0, 0, false
	}
	return left + 1, right, true
}
