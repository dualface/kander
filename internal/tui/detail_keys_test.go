package tui

import "testing"

func pinDetailLines(app *App, lines []string) {
	doc := "x"
	if app.Detail == nil {
		app.Detail = &Task{Document: doc}
	} else {
		app.Detail.Document = doc
	}
	app.Width, app.Height = 120, 40
	width := app.detailRenderWidth()
	theme := resolveTheme(app.Theme)
	key := theme + "\x00" + itoa(width) + "\x00" + doc
	plain := append([]string(nil), lines...)
	app.detailCache = detailRender{key: key, styled: plain, plain: plain}
}

func newDetailApp(lines []string, copied *[]string) *App {
	board := BoardPayload{Tasks: []Task{{TaskID: "t", Title: "t", State: "todo"}}}
	app := newApp(true, 60, pageContext{Copied: "Copied"}, func() (BoardPayload, error) {
		return board, nil
	}, func(string) (Task, error) {
		return Task{TaskID: "t", Title: "t", State: "todo", Document: "x"}, nil
	}, "dark", 40, nil, func(text string) (bool, string) {
		if copied != nil {
			*copied = append(*copied, text)
		}
		return true, ""
	})
	app.Width, app.Height = 120, 40
	app.Detail = &Task{TaskID: "t", Title: "t", State: "todo", Document: "x"}
	pinDetailLines(app, lines)
	return app
}

func TestDetailVimWordAndLineKeys(t *testing.T) {
	app := newDetailApp([]string{"hello world", "  next"}, nil)
	app.handleDetailKey("w")
	if app.DetailCursor != [2]int{0, 6} {
		t.Fatalf("w: %v", app.DetailCursor)
	}
	app.handleDetailKey("e")
	if app.DetailCursor != [2]int{0, 10} {
		t.Fatalf("e: %v", app.DetailCursor)
	}
	app.handleDetailKey("b")
	if app.DetailCursor != [2]int{0, 6} {
		t.Fatalf("b: %v", app.DetailCursor)
	}
	app.handleDetailKey("0")
	if app.DetailCursor != [2]int{0, 0} {
		t.Fatalf("0: %v", app.DetailCursor)
	}
	app.handleDetailKey("j")
	app.handleDetailKey("^")
	if app.DetailCursor != [2]int{1, 2} {
		t.Fatalf("^: %v", app.DetailCursor)
	}
	app.handleDetailKey("$")
	if app.DetailCursor != [2]int{1, 6} {
		t.Fatalf("$: %v", app.DetailCursor)
	}
}

func TestDetailCountAndZero(t *testing.T) {
	app := newDetailApp([]string{"one two three four"}, nil)
	app.handleDetailKey("3")
	app.handleDetailKey("w")
	if app.DetailCursor != [2]int{0, 14} {
		t.Fatalf("3w: %v", app.DetailCursor)
	}
	app.handleDetailKey("1")
	app.handleDetailKey("0")
	app.handleDetailKey("h")
	if app.DetailCursor[1] != 4 {
		t.Fatalf("10h from 14: %v", app.DetailCursor)
	}
	app.handleDetailKey("0")
	if app.DetailCursor != [2]int{0, 0} {
		t.Fatalf("lone 0: %v", app.DetailCursor)
	}
}

func TestDetailVisualWordCopy(t *testing.T) {
	var copied []string
	app := newDetailApp([]string{"hello world"}, &copied)
	app.handleDetailKey("v")
	app.handleDetailKey("e")
	app.handleDetailKey("y")
	if len(copied) != 1 || copied[0] != "hello" {
		t.Fatalf("ve y: %v", copied)
	}
	copied = copied[:0]
	app.DetailCursor = [2]int{0, 0}
	app.handleDetailKey("v")
	app.handleDetailKey("w")
	app.handleDetailKey("y")
	if len(copied) != 1 || copied[0] != "hello " {
		t.Fatalf("vw y: %v", copied)
	}
	copied = copied[:0]
	app.DetailCursor = [2]int{0, 0}
	app.handleDetailKey("v")
	app.handleDetailKey("$")
	app.handleDetailKey("y")
	if len(copied) != 1 || copied[0] != "hello world" {
		t.Fatalf("v$ y: %v", copied)
	}
}

func TestDetailVisualTextObjectAndFind(t *testing.T) {
	var copied []string
	app := newDetailApp([]string{`use "hi" and x`}, &copied)
	app.DetailCursor = [2]int{0, 5}
	app.handleDetailKey("v")
	app.handleDetailKey("i")
	app.handleDetailKey(`"`)
	app.handleDetailKey("y")
	if len(copied) != 1 || copied[0] != "hi" {
		t.Fatalf(`vi": %v`, copied)
	}
	copied = copied[:0]
	app.DetailCursor = [2]int{0, 0}
	app.handleDetailKey("v")
	app.handleDetailKey("f")
	app.handleDetailKey("x")
	app.handleDetailKey("y")
	if len(copied) != 1 || copied[0] != `use "hi" and x` {
		t.Fatalf("vf x: %v", copied)
	}
	cursor := app.DetailCursor
	app.handleDetailKey("f")
	app.handleDetailKey("z")
	if app.DetailFindWait != "" {
		t.Fatalf("failed f left wait=%q", app.DetailFindWait)
	}
	if app.DetailCursor != cursor {
		t.Fatalf("failed f moved cursor: %v", app.DetailCursor)
	}
}

func TestDetailYankOperator(t *testing.T) {
	var copied []string
	app := newDetailApp([]string{"hello world"}, &copied)
	app.handleDetailKey("y")
	app.handleDetailKey("i")
	app.handleDetailKey("w")
	if len(copied) != 1 || copied[0] != "hello" {
		t.Fatalf("yiw: %v", copied)
	}
	if app.DetailCursor != [2]int{0, 0} {
		t.Fatalf("yiw must keep cursor: %v", app.DetailCursor)
	}
	if app.detailSelectionActive() || app.DetailOp != "" {
		t.Fatal("yiw left pending state")
	}
	copied = copied[:0]
	app.handleDetailKey("y")
	app.handleDetailKey("y")
	if len(copied) != 1 || copied[0] != "hello world" {
		t.Fatalf("yy: %v", copied)
	}
	copied = copied[:0]
	app.DetailCursor = [2]int{0, 6}
	app.handleDetailKey("Y")
	if len(copied) != 1 || copied[0] != "world" {
		t.Fatalf("Y: %v", copied)
	}
}

func TestDetailSearchIgnoresMotions(t *testing.T) {
	app := newDetailApp([]string{"hello world"}, nil)
	app.handleDetailKey("/")
	app.handleDetailKey("w")
	app.handleDetailKey("e")
	if !app.DetailSearching || app.DetailQuery != "we" {
		t.Fatalf("search query=%q searching=%v cursor=%v", app.DetailQuery, app.DetailSearching, app.DetailCursor)
	}
	if app.DetailCursor != [2]int{0, 0} {
		t.Fatalf("search moved cursor: %v", app.DetailCursor)
	}
}

func TestDetailVisualSwapEnds(t *testing.T) {
	app := newDetailApp([]string{"hello"}, nil)
	app.handleDetailKey("v")
	app.handleDetailKey("e")
	if app.DetailCursor != [2]int{0, 5} {
		t.Fatalf("ve cursor: %v", app.DetailCursor)
	}
	app.handleDetailKey("o")
	if app.DetailCursor != [2]int{0, 0} {
		t.Fatalf("o cursor: %v", app.DetailCursor)
	}
	if app.DetailAnchor == nil || *app.DetailAnchor != [2]int{0, 5} {
		t.Fatalf("o anchor: %v", app.DetailAnchor)
	}
}

func TestDetailEscCancelsPending(t *testing.T) {
	app := newDetailApp([]string{"hello"}, nil)
	app.handleDetailKey("y")
	app.handleDetailKey("esc")
	if app.DetailOp != "" || app.Detail == nil {
		t.Fatal("esc cancelled op but closed detail")
	}
	app.handleDetailKey("2")
	app.handleDetailKey("esc")
	if app.DetailCountOn {
		t.Fatal("esc left count")
	}
}

func TestDetailEscCancelsFindWait(t *testing.T) {
	var copied []string
	app := newDetailApp([]string{"one two three four"}, &copied)
	app.handleDetailKey("y")
	app.handleDetailKey("f")
	app.handleDetailKey("esc")
	if app.DetailOp != "" || app.DetailFindWait != "" || app.DetailCountOn {
		t.Fatalf("esc left pending: op=%q wait=%q countOn=%v", app.DetailOp, app.DetailFindWait, app.DetailCountOn)
	}
	app.handleDetailKey("y")
	if len(copied) != 0 {
		t.Fatalf("yf esc y yanked: %v", copied)
	}
	if app.DetailOp != "y" {
		t.Fatalf("second y should enter operator-pending, op=%q", app.DetailOp)
	}
	app.handleDetailKey("esc")
	app.handleDetailKey("3")
	app.handleDetailKey("f")
	app.handleDetailKey("esc")
	app.handleDetailKey("w")
	if app.DetailCursor != [2]int{0, 4} {
		t.Fatalf("3f esc w should be 1w, cursor=%v", app.DetailCursor)
	}
}

func TestDetailRepeatFindKeepsLastCommand(t *testing.T) {
	app := newDetailApp([]string{"x a x b x"}, nil)
	app.handleDetailKey("f")
	app.handleDetailKey("x")
	if app.DetailCursor != [2]int{0, 4} {
		t.Fatalf("fx: %v", app.DetailCursor)
	}
	if app.DetailLastFind != "f" {
		t.Fatalf("last find after fx: %q", app.DetailLastFind)
	}
	app.handleDetailKey(",")
	if app.DetailCursor != [2]int{0, 0} {
		t.Fatalf("comma: %v", app.DetailCursor)
	}
	if app.DetailLastFind != "f" {
		t.Fatalf("comma must keep last find: %q", app.DetailLastFind)
	}
	app.handleDetailKey(";")
	if app.DetailCursor != [2]int{0, 4} {
		t.Fatalf("semicolon after comma: %v", app.DetailCursor)
	}
	if app.DetailLastFind != "f" {
		t.Fatalf("semicolon must keep last find: %q", app.DetailLastFind)
	}
}

func TestDetailYankPercentIncludesCloser(t *testing.T) {
	var copied []string
	app := newDetailApp([]string{"fn(a[0])"}, &copied)
	app.DetailCursor = [2]int{0, 7}
	app.handleDetailKey("y")
	app.handleDetailKey("%")
	if len(copied) != 1 || copied[0] != "(a[0])" {
		t.Fatalf("y%% from closer: %v", copied)
	}
	copied = copied[:0]
	app.DetailCursor = [2]int{0, 7}
	app.handleDetailKey("v")
	app.handleDetailKey("%")
	app.handleDetailKey("y")
	if len(copied) != 1 || copied[0] != "(a[0])" {
		t.Fatalf("v%% y from closer: %v", copied)
	}
}

func TestDetailLineVisualTextObjectYanksChar(t *testing.T) {
	var copied []string
	app := newDetailApp([]string{"hello world extra"}, &copied)
	app.handleDetailKey("V")
	app.handleDetailKey("b")
	app.handleDetailKey("i")
	app.handleDetailKey("w")
	if app.DetailSelectMode != "char" {
		t.Fatalf("V iw should switch to char, mode=%q", app.DetailSelectMode)
	}
	app.handleDetailKey("y")
	if len(copied) != 1 || copied[0] != "extra" {
		t.Fatalf("V b iw y: %v", copied)
	}
}

func TestDetailTillYankExcludesTarget(t *testing.T) {
	var copied []string
	app := newDetailApp([]string{"hello,world"}, &copied)
	app.handleDetailKey("y")
	app.handleDetailKey("t")
	app.handleDetailKey(",")
	if len(copied) != 1 || copied[0] != "hello" {
		t.Fatalf("yt,: %v", copied)
	}
}
