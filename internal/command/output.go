package command

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Stable error codes that the output layer itself assigns. Packages add the codes of their own
// failures through classifiers; docs/json-output.md lists every value.
const (
	CodeUsage       = "usage"
	CodeInteractive = "interactive_required"
	CodeFailed      = "failed"
)

// Notice is a remark of a command that is not an error: the text a command prints on stderr
// without --json, and an entry of the notices field of the result object with --json.
type Notice struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Failure is a failure with a stable code and its own exit status. Err is the cause; Message is
// the text shown to the user, which defaults to the text of Err.
type Failure struct {
	Code    string
	Status  int
	Message string
	Err     error
}

func (f *Failure) Error() string {
	switch {
	case f.Message != "":
		return f.Message
	case f.Err != nil:
		return f.Err.Error()
	}
	return f.Code
}

func (f *Failure) Unwrap() error { return f.Err }

// Coded wraps err with a stable code. The exit status is the one the command reports the
// failure with.
func Coded(code string, err error) error { return &Failure{Code: code, Err: err} }

// Interactive is the error of a command form that needs a terminal when --json was given.
// reason names what would have been asked.
func Interactive(reason string) error {
	return &Failure{Code: CodeInteractive, Status: 2, Message: reason}
}

// Classifier maps an error to the stable code and the localized message it is reported with.
// ok is false when the classifier does not know the error.
type Classifier func(err error) (code, message string, ok bool)

var (
	classifiersMu sync.Mutex
	classifiers   []Classifier
)

// RegisterClassifier adds a classifier. Packages call it from init; classifiers run in
// registration order and the first that knows the error wins.
func RegisterClassifier(c Classifier) {
	classifiersMu.Lock()
	defer classifiersMu.Unlock()
	classifiers = append(classifiers, c)
}

// Describe returns the stable code and the message err is reported with.
func Describe(err error) (code, message string) {
	var failure *Failure
	if errors.As(err, &failure) && failure.Code != "" {
		return failure.Code, describeMessage(failure)
	}
	var usage *UsageError
	if errors.As(err, &usage) {
		return CodeUsage, usage.Message
	}
	classifiersMu.Lock()
	known := append([]Classifier(nil), classifiers...)
	classifiersMu.Unlock()
	for _, classify := range known {
		if code, message, ok := classify(err); ok {
			return code, message
		}
	}
	return CodeFailed, err.Error()
}

// describeMessage is the message of a failure: its own text, else the classified text of its
// cause.
func describeMessage(f *Failure) string {
	if f.Message != "" || f.Err == nil {
		return f.Error()
	}
	_, message := Describe(f.Err)
	return message
}

// session is the output state of the one command a process runs. Execute resets it for every
// run, so tests that run several commands in one process do not see each other.
type session struct {
	mu          sync.Mutex
	json        bool
	provisional bool
	streaming   bool
	result      []byte
	status      int
	notices     []Notice
	failure     error
	reported    bool
}

var current session

func (s *session) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.json, s.provisional, s.streaming = false, false, false
	s.result, s.status, s.notices, s.failure, s.reported = nil, 0, nil, nil, false
}

// JSON reports whether the running command was given --json.
func JSON() bool {
	current.mu.Lock()
	defer current.mu.Unlock()
	return current.json
}

// jsonFlag is the global --json flag. It records the parsed value in the output session.
type jsonFlag struct{}

func (jsonFlag) String() string { return "false" }
func (jsonFlag) Type() string   { return "bool" }
func (jsonFlag) Set(raw string) error {
	on := raw != "false"
	current.mu.Lock()
	current.json = on
	current.mu.Unlock()
	return nil
}

func addJSONFlag(root *cobra.Command) {
	f := root.PersistentFlags().VarPF(jsonFlag{}, "json", "", "")
	f.NoOptDefVal = "true"
}

// scanJSON reports whether args name --json. It only decides the shape of the error of a
// command line that did not parse; a parsed command line answers through JSON.
func scanJSON(args []string) bool {
	var flags pflag.FlagSet
	flags.Init("json", pflag.ContinueOnError)
	flags.SetInterspersed(true)
	flags.SetOutput(io.Discard)
	flags.ParseErrorsAllowlist.UnknownFlags = true
	flags.BoolP("help", "h", false, "")
	value := flags.Bool("json", false, "")
	_ = flags.Parse(args)
	return *value
}

// NoJSON is the argument error of a command that has no JSON form: the bare command and the
// internal worker. It returns nil without --json.
func NoJSON() error {
	if JSON() {
		return Usage("cli.error_json_unsupported")
	}
	return nil
}

// Streaming marks c as a command that keeps running and writes as it goes: its stdout is not
// buffered, and a failure after output started adds an error object to stderr.
func Streaming(c *cobra.Command) *cobra.Command {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[streamingAnnotation] = "true"
	return c
}

const streamingAnnotation = "kander/streaming"

// Result records the result object of the running command. The object is written to stdout when
// the command ends, so a command that fails after calling Result writes nothing. v must encode
// as a JSON object. It returns the exit status a RunE passes to Exit.
func Result(v any) int { return ResultStatus(v, 0) }

// ResultStatus is Result for a command whose contract ties an exit status to a finished
// analysis (check, coordinator reconcile): the object is the result, the status is not a
// failure.
func ResultStatus(v any, status int) int {
	data, err := json.Marshal(v)
	if err == nil && !bytes.HasPrefix(data, []byte("{")) {
		err = errors.New("the result of a command must be a JSON object")
	}
	if err != nil {
		return Fail(err)
	}
	current.mu.Lock()
	current.result, current.status = data, status
	current.mu.Unlock()
	return status
}

// Object writes the object a command always prints: with --json it is the result, without it
// one JSON line on stdout.
func Object(v any) int { return ObjectStatus(v, 0) }

// ObjectStatus is Object for a command whose contract ties an exit status to the object.
func ObjectStatus(v any, status int) int {
	if JSON() {
		return ResultStatus(v, status)
	}
	if err := json.NewEncoder(os.Stdout).Encode(v); err != nil {
		return Fail(err)
	}
	return status
}

// Emit writes one JSON object line to stdout at once. A streaming command uses it for what it
// has to say while it runs: the lines of a stream, the object of the moment it can serve.
func Emit(v any) int {
	data, err := json.Marshal(v)
	if err != nil {
		return Fail(err)
	}
	if _, err := os.Stdout.Write(append(data, '\n')); err != nil {
		return Fail(err)
	}
	return 0
}

// Notify reports a remark of the running command. Without --json, text is written to stderr as
// given (a trailing newline is added when it has none); with --json the remark joins the
// notices of the result object, or on a streaming command becomes a line of its own.
func Notify(code, text string) {
	if !JSON() {
		fmt.Fprint(os.Stderr, withNewline(text))
		return
	}
	notice := Notice{Code: code, Message: strings.TrimRight(text, "\r\n")}
	current.mu.Lock()
	streaming := current.streaming
	if !streaming {
		current.notices = append(current.notices, notice)
	}
	current.mu.Unlock()
	if streaming {
		writeLine(os.Stderr, map[string]any{"notice": notice})
	}
}

// NoticeWriter returns a writer for code that prints its remarks to a writer: each Write
// becomes one Notify with the given code.
func NoticeWriter(code string) io.Writer { return noticeWriter(code) }

type noticeWriter string

func (w noticeWriter) Write(data []byte) (int, error) {
	Notify(string(w), string(data))
	return len(data), nil
}

func withNewline(text string) string {
	if strings.HasSuffix(text, "\n") {
		return text
	}
	return text + "\n"
}

// Fail reports err as the failure of the running command and returns exit status 1.
func Fail(err error) int { return FailStatus(1, err) }

// FailStatus is Fail with an exit status of the command's own.
func FailStatus(status int, err error) int {
	var failure *Failure
	if errors.As(err, &failure) && failure.Status != 0 {
		status = failure.Status
	}
	current.mu.Lock()
	if current.failure == nil {
		current.failure = err
	}
	jsonMode := current.json
	current.mu.Unlock()
	if !jsonMode {
		_, message := Describe(err)
		fmt.Fprintf(os.Stderr, "kander: %s\n", message)
	}
	return status
}

// ErrorLine is the line a failure is reported with: with --json the error object, otherwise
// the text "kander: message". Commands that write their own diagnostics (a stream that must
// not block on a full pipe) write it themselves and then call FailReported.
func ErrorLine(err error) string {
	code, message := Describe(err)
	if !JSON() {
		return "kander: " + message
	}
	data, _ := json.Marshal(map[string]any{"error": Notice{Code: code, Message: message}})
	return string(data)
}

// FailReported records err as the failure of the running command like Fail, for a command that
// has written the failure itself: nothing more is printed. It returns exit status 1.
func FailReported(err error) int {
	current.mu.Lock()
	if current.failure == nil {
		current.failure = err
	}
	current.reported = true
	current.mu.Unlock()
	return 1
}

func writeLine(w io.Writer, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = w.Write(append(data, '\n'))
}

// finish ends the run of c and returns the process exit code. Without --json it prints what the
// error of the command needs; with --json it writes the result object to stdout, or the error
// object to stderr.
func finish(c *cobra.Command, err error) int {
	current.mu.Lock()
	jsonMode := current.json || (current.provisional && err != nil && isUsage(err))
	streaming := current.streaming
	result, status, notices, failure := current.result, current.status, current.notices, current.failure
	reported := current.reported
	current.mu.Unlock()

	if !jsonMode {
		return Report(c, err)
	}
	if err == nil {
		if !streaming || len(result) > 0 {
			writeResult(result, notices)
		}
		return status
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		if failure == nil {
			if len(result) > 0 {
				writeResult(result, notices)
				return exit.Code
			}
			failure = fmt.Errorf("exit status %d", exit.Code)
		}
		err = failure
		status = exit.Code
	} else {
		status = 1
	}
	var f *Failure
	var usage *UsageError
	switch {
	case errors.As(err, &usage):
		status = 2
	case errors.As(err, &f) && f.Status != 0:
		status = f.Status
	}
	if reported {
		return status
	}
	code, message := Describe(err)
	object := map[string]any{"error": Notice{Code: code, Message: message}}
	if len(notices) > 0 && !streaming {
		object["notices"] = notices
	}
	data, _ := json.Marshal(object)
	fmt.Fprintln(os.Stderr, string(data))
	return status
}

func isUsage(err error) bool {
	var usage *UsageError
	return errors.As(err, &usage)
}

// writeResult writes the result object with the notices spliced in as a last field.
func writeResult(result []byte, notices []Notice) {
	if len(result) == 0 {
		result = []byte("{}")
	}
	if len(notices) > 0 {
		extra, _ := json.Marshal(notices)
		body := bytes.TrimSuffix(result, []byte("}"))
		if len(bytes.TrimSpace(body)) > 1 {
			body = append(body, ',')
		}
		result = append(append(append(body, []byte(`"notices":`)...), extra...), '}')
	}
	_, _ = os.Stdout.Write(append(result, '\n'))
}

// begin resets the session for a run of args.
func begin(args []string) {
	current.reset()
	current.mu.Lock()
	current.provisional = scanJSON(args)
	current.mu.Unlock()
}

// markStreaming records that the command about to run keeps writing as it goes.
func markStreaming(c *cobra.Command) {
	if c.Annotations[streamingAnnotation] == "true" {
		current.mu.Lock()
		current.streaming = true
		current.mu.Unlock()
	}
}
