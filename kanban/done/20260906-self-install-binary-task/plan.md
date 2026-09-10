# 实施计划

工作目录: `/home/dualf/works/kander/worktrees/self-install-binary`
任务分支: `self-install-binary` (base `586852545206645bf6fd09e6980651aba268f61e`)
交付目标: `develop`

## 步骤

1. 将 `rules/*.md` 移入 `rules/cn/`, 新增 `rules/en/README.txt` 占位, 让 `rules/` 成为 `go:embed` 包 (`Names` / `File` / `Hash`).
2. 在 `internal/fs` 增加可执行二进制原子写入与相对符链/硬链, 安装路径全部经固定父句柄.
3. 新增 `internal/install`: 交互向导、自拷贝、规则释出、git exclude、残留清理、规则 stamp、doctor 修复.
4. 接线 `kander install`; 裸 `kander` 首次运行进向导; 交接后 doctor → options, 无看板时降级空看板.
5. `repairValues` 尊重 `--lang` / CLI 语言, 避免交接后语言被抹成 en.
6. doctor 检查缺失/过期并修复; 用户手改不覆盖; 语言漂移只 note.
7. 删除 `install.sh` / `install.ps1` 及脚本驱动测试, 文档与 Makefile 同步.

## 影响模块

`rules/`, `internal/fs`, `internal/config`, `internal/install` (新), `internal/cli`, `internal/menu`, `internal/tui`, `internal/i18n`, `AGENTS.md`, `README.md`, `Makefile`.

## 验证

```sh
go build ./... && go test ./... && go vet ./...
gofmt -l .
git diff --check HEAD
```

Windows 占用 exe 改名让位在非 Windows 上 skip, 由 `TestWriteBinaryBusyRenameAside` 守卫.

## 发布与回滚

集成到 `develop` 后, 旧 `install.sh`/`install.ps1` 不再存在. 回滚即还原该提交之前的 `develop`. 已安装用户不受影响, 可用 `kander install` 或 doctor 对齐规则.
