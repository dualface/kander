package install

// previousOfficialHashes are SHA-256 digests of rule files shipped by install.sh /
// install.ps1 immediately before self-install. Doctor uses them to tell outdated
// official copies from local edits when kander-rules-state.json is absent.
var previousOfficialHashes = map[string][]string{
	"KANDER-AGENTS.md":              {"2b4acbb0278eca67217ec1b8a7c0fc023f425992895b6594435becde4ee16656"},
	"KANDER-BASE-RULES.md":          {"519b5b65e3c147b98185b0374e5c51365e3dfbe19be1d57b73694a78183e4935"},
	"KANDER-CODE-RULES.md":          {"3b1f82396e4f646721ef8ae4c57ba98e0caa9731faea8ba42309677e583525b4"},
	"KANDER-COLLABORATION-RULES.md": {"9a5be4f188c7a5ee5721d074420027c016fc2d4139da18976ebc4c3f8da958fc"},
	"KANDER-GIT-RULES.md":           {"65b51a2ed0f474d275c43715998a50a08f285c469aaf696c1c6867ec79aeab75"},
	"KANDER-KANBAN-RULES.md":        {"aa8fe364854ccce3b82494d70cb61e53c7eeb21227a1d326cdecb39e396d5662"},
	"KANDER-REPORTING-RULES.md":     {"5743b7f4ae40a2d665ad89e00243f65b8ddaab6b3c20833be8c100922172ac62"},
	"KANDER-REVIEW-RULES.md":        {"84f31f4dcc2cfc4d44eefe811f60f523c08be3e4d62d85ba303ce54f4d177abf"},
	"KANDER-TASK-GROUP-RULES.md":    {"dfcacf126b2766018d8010055fc7e9e95b88407c17c47b31299788c85d52b251"},
	"KANDER-TASK-INTAKE-RULES.md":   {"9596d928014a15746a8d088775045674c09b9461e077458d022cbb306a0c002d"},
}

func isPreviousOfficial(name, digest string) bool {
	for _, known := range previousOfficialHashes[name] {
		if known == digest {
			return true
		}
	}
	return false
}
