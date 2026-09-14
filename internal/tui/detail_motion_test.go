package tui

import "testing"

func TestWordMotions(t *testing.T) {
	lines := []string{"hello world", "你好,x"}
	if got := wordForward(lines, detailPos{0, 0}, false); got != (detailPos{0, 6}) {
		t.Fatalf("w from start: %+v", got)
	}
	if got := wordForward(lines, detailPos{0, 4}, false); got != (detailPos{0, 6}) {
		t.Fatalf("w from middle: %+v", got)
	}
	if got := wordForward(lines, detailPos{0, 6}, false); got != (detailPos{1, 0}) {
		t.Fatalf("w across line: %+v", got)
	}
	if got := wordBack(lines, detailPos{0, 6}, false); got != (detailPos{0, 0}) {
		t.Fatalf("b to first word: %+v", got)
	}
	if got := wordBack(lines, detailPos{0, 3}, false); got != (detailPos{0, 0}) {
		t.Fatalf("b from middle: %+v", got)
	}
	if got := wordEnd(lines, detailPos{0, 0}, false); got != (detailPos{0, 4}) {
		t.Fatalf("e from start: %+v", got)
	}
	if got := wordEnd(lines, detailPos{0, 4}, false); got != (detailPos{0, 10}) {
		t.Fatalf("e to next word end: %+v", got)
	}
	if got := wordForward(lines, detailPos{1, 0}, false); got != (detailPos{1, 2}) {
		t.Fatalf("w CJK then punct: %+v", got)
	}
	if got := wordForward(lines, detailPos{1, 2}, false); got != (detailPos{1, 3}) {
		t.Fatalf("w punct then latin: %+v", got)
	}
	start := detailPos{1, 3}
	if got := wordForward(lines, start, false); got != start {
		t.Fatalf("w at last word: %+v", got)
	}
}

func TestWORDMotions(t *testing.T) {
	lines := []string{"foo.bar baz"}
	if got := wordForward(lines, detailPos{0, 0}, true); got != (detailPos{0, 8}) {
		t.Fatalf("W skips punct: %+v", got)
	}
	if got := wordEnd(lines, detailPos{0, 0}, true); got != (detailPos{0, 6}) {
		t.Fatalf("E to WORD end: %+v", got)
	}
}

func TestLineMotions(t *testing.T) {
	lines := []string{"  hello", ""}
	if got := lineBegin(detailPos{0, 5}); got != (detailPos{0, 0}) {
		t.Fatalf("0: %+v", got)
	}
	if got := lineFirstNonBlank(lines, detailPos{0, 5}); got != (detailPos{0, 2}) {
		t.Fatalf("^: %+v", got)
	}
	if got := lineFirstNonBlank(lines, detailPos{1, 0}); got != (detailPos{1, 0}) {
		t.Fatalf("^ blank: %+v", got)
	}
	if got := lineEnd(lines, detailPos{0, 0}); got != (detailPos{0, 7}) {
		t.Fatalf("$: %+v", got)
	}
}

func TestParagraphMotions(t *testing.T) {
	lines := []string{"aaa", "", "bbb", "", "ccc"}
	if got := paraForward(lines, detailPos{0, 1}); got != (detailPos{1, 0}) {
		t.Fatalf("}: %+v", got)
	}
	if got := paraForward(lines, detailPos{1, 0}); got != (detailPos{3, 0}) {
		t.Fatalf("} from blank: %+v", got)
	}
	if got := paraBack(lines, detailPos{2, 1}); got != (detailPos{1, 0}) {
		t.Fatalf("{: %+v", got)
	}
	if got := paraBack(lines, detailPos{0, 0}); got != (detailPos{0, 0}) {
		t.Fatalf("{ at top: %+v", got)
	}
}

func TestMatchBracket(t *testing.T) {
	lines := []string{"fn(a[0])"}
	got, ok := matchBracket(lines, detailPos{0, 2})
	if !ok || got != (detailPos{0, 7}) {
		t.Fatalf("percent from (: %+v ok=%v", got, ok)
	}
	got, ok = matchBracket(lines, detailPos{0, 4})
	if !ok || got != (detailPos{0, 6}) {
		t.Fatalf("percent from [: %+v ok=%v", got, ok)
	}
	if _, ok := matchBracket([]string{"plain text"}, detailPos{0, 0}); ok {
		t.Fatal("percent with no bracket must not move")
	}
	nested := []string{"{", "  (x)", "}"}
	got, ok = matchBracket(nested, detailPos{0, 0})
	if !ok || got != (detailPos{2, 0}) {
		t.Fatalf("percent nested: %+v ok=%v", got, ok)
	}
}

func TestFindChar(t *testing.T) {
	lines := []string{"abxcxd"}
	got, ok := findChar(lines, detailPos{0, 0}, 'x', true, false, 1)
	if !ok || got != (detailPos{0, 2}) {
		t.Fatalf("f: %+v ok=%v", got, ok)
	}
	got, ok = findChar(lines, detailPos{0, 0}, 'x', true, true, 1)
	if !ok || got != (detailPos{0, 1}) {
		t.Fatalf("t: %+v ok=%v", got, ok)
	}
	got, ok = findChar(lines, detailPos{0, 0}, 'x', true, false, 2)
	if !ok || got != (detailPos{0, 4}) {
		t.Fatalf("2f: %+v ok=%v", got, ok)
	}
	if _, ok := findChar(lines, detailPos{0, 0}, 'z', true, false, 1); ok {
		t.Fatal("missing f must fail")
	}
	got, ok = findChar(lines, detailPos{0, 5}, 'x', false, false, 1)
	if !ok || got != (detailPos{0, 4}) {
		t.Fatalf("F: %+v ok=%v", got, ok)
	}
}

func TestQuoteAndWordObjects(t *testing.T) {
	start, end, ok := quoteRange(`say "hi" now`, 5, '"', false, 1)
	if !ok || start != 5 || end != 7 {
		t.Fatalf(`i": %d %d ok=%v`, start, end, ok)
	}
	start, end, ok = quoteRange(`say "hi" now`, 5, '"', true, 1)
	if !ok || start != 4 || end != 8 {
		t.Fatalf(`a": %d %d ok=%v`, start, end, ok)
	}
	start, end, ok = quoteRange("use `code` here", 5, '`', false, 1)
	if !ok || start != 5 || end != 9 {
		t.Fatalf("i`: %d %d ok=%v", start, end, ok)
	}
	lines := []string{"hello world"}
	s, e, ok := wordRange(lines, detailPos{0, 1}, false, false, 1)
	if !ok || s != (detailPos{0, 0}) || e != (detailPos{0, 5}) {
		t.Fatalf("iw: %+v %+v ok=%v", s, e, ok)
	}
	s, e, ok = wordRange(lines, detailPos{0, 1}, false, true, 1)
	if !ok || s != (detailPos{0, 0}) || e != (detailPos{0, 6}) {
		t.Fatalf("aw: %+v %+v ok=%v", s, e, ok)
	}
}

func TestVisualExclusiveEnd(t *testing.T) {
	lines := []string{"hello"}
	origin := detailPos{0, 0}
	got := visualExclusiveEnd(lines, origin, detailPos{0, 4})
	if got != (detailPos{0, 5}) {
		t.Fatalf("inclusive forward: %+v", got)
	}
	got = visualExclusiveEnd(lines, origin, detailPos{0, 5})
	if got != (detailPos{0, 5}) {
		t.Fatalf("already EOL: %+v", got)
	}
	got = visualExclusiveEnd(lines, detailPos{0, 4}, detailPos{0, 0})
	if got != (detailPos{0, 0}) {
		t.Fatalf("backward helper leaves dest: %+v", got)
	}
	if got := bumpExclusive(lines, detailPos{0, 4}); got != (detailPos{0, 5}) {
		t.Fatalf("bump origin for inclusive reverse: %+v", got)
	}
}
