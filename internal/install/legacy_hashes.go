package install

// previousOfficialHashes are SHA-256 digests of rule files shipped by install.sh /
// install.ps1 immediately before self-install, plus the final Chinese originals shipped
// before the rules became English-only. Doctor uses them to tell outdated official
// copies from local edits when kander-rules-state.json is absent.
var previousOfficialHashes = map[string][]string{
	"KANDER-AGENTS.md":              {"2b4acbb0278eca67217ec1b8a7c0fc023f425992895b6594435becde4ee16656"},
	"KANDER-BASE-RULES.md":          {"519b5b65e3c147b98185b0374e5c51365e3dfbe19be1d57b73694a78183e4935", "a31cefdcfd6530563354255eb18479045d487fc356757e593518c3c30209ed61"},
	"KANDER-CODE-RULES.md":          {"3b1f82396e4f646721ef8ae4c57ba98e0caa9731faea8ba42309677e583525b4"},
	"KANDER-COLLABORATION-RULES.md": {"9a5be4f188c7a5ee5721d074420027c016fc2d4139da18976ebc4c3f8da958fc"},
	"KANDER-GIT-RULES.md":           {"65b51a2ed0f474d275c43715998a50a08f285c469aaf696c1c6867ec79aeab75"},
	"KANDER-KANBAN-RULES.md":        {"aa8fe364854ccce3b82494d70cb61e53c7eeb21227a1d326cdecb39e396d5662", "c113e309a00931769400b0016f4df8e2e0d5722a9b6a278db34ec97b8f34aaa9"},
	"KANDER-REPORTING-RULES.md":     {"5743b7f4ae40a2d665ad89e00243f65b8ddaab6b3c20833be8c100922172ac62"},
	"KANDER-REVIEW-RULES.md":        {"84f31f4dcc2cfc4d44eefe811f60f523c08be3e4d62d85ba303ce54f4d177abf", "9a22020d079d00247ed3cfcf6e2cd2c9ab1894170cdb4e1663896f6781cbe938"},
	"KANDER-TASK-GROUP-RULES.md":    {"dfcacf126b2766018d8010055fc7e9e95b88407c17c47b31299788c85d52b251", "0fbb6ca5a497d23769f5e82dcda08803b91ff18caf00d984536daed693428e12"},
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
