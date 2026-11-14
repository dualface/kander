import KanderFormal

/-
kander-formal: regenerates the Go decision kernels and the differential vectors.

  kander-formal gen <repo-root> <out-root>

reads each registered machine's definition file below <repo-root>/formal and
writes internal/statemachine/<machine>/{zz_generated.go, zz_generated_test.go,
testdata/vectors.jsonl} below <out-root>. scripts/formal-gen.sh passes the
repository root twice; scripts/formal-check.sh writes into a temporary
directory and compares.
-/
open KanderFormal KanderFormal.Gen KanderFormal.Registry

def writeOut (root : System.FilePath) (rel : String) (content : String) : IO Unit := do
  let path := root / rel
  if let some dir := path.parent then IO.FS.createDirAll dir
  IO.FS.writeFile path content
  IO.println rel

def gen (repo out : System.FilePath) : IO Unit := do
  for en in entries do
    let m := en.machine
    let digest := sha256Hex (← IO.FS.readBinFile (repo / "formal" / en.source))
    let kernel ← IO.ofExcept ((kernelFile m en.source digest).mapError IO.userError)
    let dir := "internal/statemachine/" ++ m.name ++ "/"
    writeOut out (dir ++ "zz_generated.go") kernel
    writeOut out (dir ++ "zz_generated_test.go") (testFile en)
    writeOut out (dir ++ "testdata/vectors.jsonl") ("\n".intercalate (vectorLines en) ++ "\n")

def main (args : List String) : IO UInt32 := do
  match args with
  | ["gen", repo, out] => gen repo out; return 0
  | _ =>
    IO.eprintln "usage: kander-formal gen <repo-root> <out-root>"
    return 2
