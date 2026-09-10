The report MUST contain exactly one ```kander-findings fenced JSON object with both FINDINGS and NON_BLOCKING arrays. A prose-only report fails validation and cannot close the batch.

Review focus:
(1) 合同变更: version.String() 只返回语义版号; kander version 输出变为 kander <semver|dev>; 评审清单与 TUI 不再带 commit 信息 (用户已接受).
(2) 发版门闸: Release workflow 仅在推送 v* tag 时运行; tag_name 必须是 github.ref_name, 禁止再拼 v 前缀导致 vv0.5.0; 版本取 GITHUB_REF_NAME#v 并只注入 Version.
(3) 本地构建: Makefile / make-windows.cmd 默认 git describe --tags --always; 无 tag 或非 git 目录回退 dev 且构建不失败. 产物名与打包方式必须与改动前一致.

Verification records:
- git diff --check aac6f2bf2294fdaec863b9fa2ddfab5718ea2b78 5172edad0c23c2f85265461d6238215f443849a9: clean
- go vet ./... && go test ./... -count=1 at 5172edad0c23c2f85265461d6238215f443849a9: 21 packages ok
- make VERSION=0.5.0 && ./kander version => kander 0.5.0
- describe fallback: non-git directory => dev; git repo with no tags => dev
- release.yml packaging loop unchanged: windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; windows zip, others tar.gz; sha256sum -- * > checksums.txt
- worktree grep BuildTimestamp|GitHash: no matches

Environment gaps:
- Windows native make-windows.cmd 未在本机执行; 与 Makefile 对照逻辑等价.
- 端到端「推送 v0.5.0 后 latest 下载 URL 返回 200」将在评审通过并合入 develop 后执行, 不在本提交的本地验证范围内.