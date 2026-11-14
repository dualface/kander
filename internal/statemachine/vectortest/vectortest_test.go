package vectortest

import (
	"strings"
	"testing"
)

// counter is a toy kernel: "inc" adds one up to the limit, "reset" returns
// to zero and is refused at zero.
type counterParams struct{ limit uint64 }

type counterEvent string

func counterMachine() Machine[counterParams, uint64, counterEvent] {
	return Machine[counterParams, uint64, counterEvent]{
		Name:    "counter",
		Params:  []counterParams{{limit: 1}},
		Events:  []counterEvent{"inc", "reset"},
		Initial: func(counterParams) uint64 { return 0 },
		Step: func(s uint64, e counterEvent, p counterParams) (uint64, bool) {
			switch {
			case e == "inc" && s < p.limit:
				return s + 1, true
			case e == "reset" && s > 0:
				return 0, true
			}
			return s, false
		},
		EncodeParams:  func(p counterParams) string { return Object("limit", Uint(p.limit)) },
		EncodeState:   func(s uint64) string { return Object("n", Uint(s)) },
		EncodeEvent:   func(e counterEvent) string { return Event(string(e)) },
		EncodeQueries: func(s uint64, _ counterParams) string { return Object("zero", Bool(s == 0)) },
	}
}

const counterVectors = `{"machine":"counter","depth":2,"params":[{"limit":1}],"events":[{"event":"inc","args":{}},{"event":"reset","args":{}}]}
{"params":0,"state":{"n":0},"event":0,"accepted":true,"next":{"n":1},"queries":{"zero":true}}
{"params":0,"state":{"n":0},"event":1,"accepted":false,"next":{"n":0},"queries":{"zero":true}}
{"params":0,"state":{"n":1},"event":0,"accepted":false,"next":{"n":1},"queries":{"zero":false}}
{"params":0,"state":{"n":1},"event":1,"accepted":true,"next":{"n":0},"queries":{"zero":false}}
`

func TestCheckAcceptsMatchingVectors(t *testing.T) {
	steps, err := Check(strings.NewReader(counterVectors), counterMachine())
	if err != nil {
		t.Fatal(err)
	}
	// Two events at depth 1 and four at depth 2.
	if steps != 6 {
		t.Fatalf("steps = %d, want 6", steps)
	}
}

func TestCheckRejectsMismatches(t *testing.T) {
	cases := []struct {
		name, old, new, want string
	}{
		{"machine", `"machine":"counter"`, `"machine":"other"`, `want "counter"`},
		{"depth", `"depth":2`, `"depth":0`, "below 1"},
		{"params", `"params":[{"limit":1}]`, `"params":[{"limit":2}]`, "parameter set 0"},
		{"event", `{"event":"reset","args":{}}]`, `{"event":"clear","args":{}}]`, "event 1"},
		{"accepted", `"event":1,"accepted":false`, `"event":1,"accepted":true`, "accepted Go false, Lean true"},
		{"next", `"accepted":true,"next":{"n":1}`, `"accepted":true,"next":{"n":2}`, "next state"},
		{"queries", `"queries":{"zero":false}}` + "\n" + `{"params":0,"state":{"n":1},"event":1`, `"queries":{"zero":true}}` + "\n" + `{"params":0,"state":{"n":1},"event":1`, "queries"},
		{"missing", `{"params":0,"state":{"n":1},"event":0,"accepted":false,"next":{"n":1},"queries":{"zero":false}}` + "\n", "", "no Lean row"},
		{"duplicate", `{"params":0,"state":{"n":0},"event":1,`, `{"params":0,"state":{"n":0},"event":0,`, "duplicate row"},
		{"index", `{"params":0,"state":{"n":0},"event":1,`, `{"params":0,"state":{"n":0},"event":7,`, "out of range"},
		{"unreachable", "", `{"params":0,"state":{"n":5},"event":0,"accepted":false,"next":{"n":5},"queries":{"zero":false}}` + "\n", "not reachable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := counterVectors
			if tc.old == "" {
				data += tc.new
			} else {
				if !strings.Contains(data, tc.old) {
					t.Fatalf("fixture does not contain %q", tc.old)
				}
				data = strings.Replace(data, tc.old, tc.new, 1)
			}
			_, err := Check(strings.NewReader(data), counterMachine())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestCheckRejectsEmptyFile(t *testing.T) {
	if _, err := Check(strings.NewReader(""), counterMachine()); err == nil {
		t.Fatal("empty file accepted")
	}
}

func TestEncoders(t *testing.T) {
	got := Object("a", Array(Str("x"), Str("y")), "b", Event("go", "n", Uint(3), "ok", Bool(true)))
	want := `{"a":["x","y"],"b":{"event":"go","args":{"n":3,"ok":true}}}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
