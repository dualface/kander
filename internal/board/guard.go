package board

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GuardVerdict 是 guard-write 的判定结果.
type GuardVerdict struct {
	Allowed bool
	Reason  string
}

// GuardWrite 判定对 path 的写入是否会在看板状态目录复活旧卡.
// 状态目录直接子项 (卡片入口) 当前不存在时拒绝: 合法新建只经 kander new,
// 缺失的入口意味着卡片已迁移或从未存在, 原地新建会产生跨状态副本.
// 目录卡内部文件同样要求所属目录卡入口存在, 否则写入会静默重建整个目录卡.
// root 为空表示当前环境定位不到看板, 一律放行.
func GuardWrite(root, path string) (GuardVerdict, error) {
	if root == "" {
		return GuardVerdict{Allowed: true}, nil
	}
	abs, err := absoluteUserPath(path)
	if err != nil {
		return GuardVerdict{}, err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return GuardVerdict{Allowed: true}, nil
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return GuardVerdict{Allowed: true}, nil
	}
	state := parts[0]
	if _, ok := stateSet[state]; !ok {
		return GuardVerdict{Allowed: true}, nil
	}
	entryPath := filepath.Join(root, state, parts[1])
	if _, err := os.Lstat(entryPath); err == nil {
		return GuardVerdict{Allowed: true}, nil
	} else if !os.IsNotExist(err) {
		return GuardVerdict{}, err
	}
	taskID := strings.TrimSuffix(parts[1], ".md")
	for _, other := range States {
		if other == state {
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, other, taskID)); err == nil {
			return GuardVerdict{Reason: t("board.guard_task_moved", taskID, other)}, nil
		}
		if _, err := os.Lstat(filepath.Join(root, other, taskID+".md")); err == nil {
			return GuardVerdict{Reason: t("board.guard_task_moved", taskID, other)}, nil
		}
	}
	return GuardVerdict{Reason: t("board.guard_missing_direct_child", state)}, nil
}

// RunGuardWrite 实现 kander guard-write. 退出码 0 放行, 1 拒绝, 2 用法错误.
func RunGuardWrite(args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return usageFail("guard-write", "board.guard_path_required")
	}
	root, err := BoardRoot()
	if err != nil {
		// 定位不到看板 (非 Git 项目且未设 KANBAN_DIR) 时不阻塞宿主项目的写入;
		// 其他定位错误 (非法路径, 不安全入口) 仍然失败, 不静默放行.
		if IsBoardNotFound(err) {
			return 0
		}
		return fail(err)
	}
	verdict, err := GuardWrite(root, args[0])
	if err != nil {
		return fail(err)
	}
	if verdict.Allowed {
		return 0
	}
	fmt.Fprintln(os.Stderr, t("board.guard_rejected", args[0], verdict.Reason))
	return 1
}
