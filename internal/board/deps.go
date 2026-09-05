package board

import (
	"path/filepath"
	"sort"
	"strings"
)

func taskGroupMembers(documents map[string]string) map[string][]string {
	members := map[string][]string{}
	for taskID, text := range documents {
		if group := taskGroupFrom(text); group != "" {
			members[group] = append(members[group], taskID)
		}
	}
	for group, ids := range members {
		sort.Strings(ids)
		members[group] = ids
	}
	return members
}

// TaskDependenciesOf 解析并分类依赖; 任务组引用展开为该组当前全部成员卡.
func TaskDependenciesOf(entry Entry, board Board, documents map[string]string) (TaskDependencies, error) {
	texts := documents
	if texts == nil {
		texts = map[string]string{}
		for taskID, candidate := range board.Entries {
			text, err := ReadDocument(candidate)
			if err != nil {
				return TaskDependencies{}, err
			}
			texts[taskID] = text
		}
	}
	sourceGroup := taskGroupFrom(texts[entry.TaskID])
	if sourceGroup == "" {
		return TaskDependencies{}, nil
	}
	prerequisiteIDs, err := prerequisiteIDsFrom(texts[entry.TaskID], entry.TaskID)
	if err != nil {
		return TaskDependencies{}, err
	}
	groupMembers := taskGroupMembers(texts)
	var internalTasks, externalTasks, groups, expanded []string
	for _, prerequisiteID := range prerequisiteIDs {
		if taskGroupRe.MatchString(prerequisiteID) {
			members := groupMembers[prerequisiteID]
			if len(members) == 0 {
				return TaskDependencies{}, kanbanError(
					"board.task_references_a_missing_prerequisite_id", entry.TaskID, prerequisiteID,
				)
			}
			groups = append(groups, prerequisiteID)
			expanded = append(expanded, members...)
			continue
		}
		prerequisite, ok := board.Entries[prerequisiteID]
		if !ok {
			return TaskDependencies{}, kanbanError(
				"board.task_references_a_missing_prerequisite_id", entry.TaskID, prerequisiteID,
			)
		}
		prerequisiteGroup := taskGroupFrom(texts[prerequisite.TaskID])
		if prerequisiteGroup == sourceGroup {
			internalTasks = append(internalTasks, prerequisiteID)
		} else {
			externalTasks = append(externalTasks, prerequisiteID)
		}
		expanded = append(expanded, prerequisiteID)
	}
	return TaskDependencies{
		PrerequisiteIDs: prerequisiteIDs,
		InternalTasks:   internalTasks,
		ExternalTasks:   externalTasks,
		TaskGroups:      groups,
		ExpandedTaskIDs: uniqueKeepOrder(expanded),
	}, nil
}

func boardPathState(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	state := strings.Split(rel, string(filepath.Separator))[0]
	if _, ok := stateSet[state]; ok {
		return state
	}
	return ""
}

func problemInCheckScope(root string, problem Problem, includeAll bool) bool {
	if includeAll {
		return true
	}
	candidates := append([]string{problem.Path}, problem.RelatedPaths...)
	for _, candidate := range candidates {
		if _, deferred := deferredCheckStates[boardPathState(root, candidate)]; !deferred {
			return true
		}
	}
	return false
}

var contractCheckStates = map[string]struct{}{"todo": {}, "working": {}, "review": {}}

// contractProblems 检查已承诺执行的卡 (todo/working/review) 的契约完整性:
// 必填章节缺失或残留 <填写> 占位符, 以及验收条件没有可判定条目.
// backlog 卡尚在起草不检查; done/archived 已收口不检查.
func contractProblems(board Board) []Problem {
	ids := make([]string, 0, len(board.Entries))
	for taskID := range board.Entries {
		ids = append(ids, taskID)
	}
	sort.Strings(ids)
	var problems []Problem
	for _, taskID := range ids {
		entry := board.Entries[taskID]
		if _, ok := contractCheckStates[entry.State]; !ok {
			continue
		}
		text, err := ReadDocument(entry)
		if err != nil {
			// 文档不可读由结构扫描或依赖检查报告, 这里不重复.
			continue
		}
		var defects []string
		if missing := incompleteReadySections(text); len(missing) > 0 {
			defects = append(defects, t("board.contract_sections_incomplete", strings.Join(missing, ", ")))
		}
		if lacksAcceptanceItems(text) {
			defects = append(defects, t("board.task_requires_acceptance_items"))
		}
		for _, defect := range defects {
			problems = append(problems, Problem{
				Path:    entry.Document,
				Message: t("board.contract_defect", taskID, defect),
			})
		}
	}
	return problems
}

func dependencyProblems(board Board, selected map[string]struct{}, skipStates map[string]struct{}) []Problem {
	documents := map[string]string{}
	documentErrors := map[string]error{}
	for taskID, entry := range board.Entries {
		text, err := ReadDocument(entry)
		if err != nil {
			documentErrors[taskID] = err
			continue
		}
		documents[taskID] = text
	}
	resolved := map[string]TaskDependencies{}
	resolutionErrors := map[string]error{}

	resolve := func(taskID string) (TaskDependencies, bool) {
		if value, ok := resolved[taskID]; ok {
			return value, true
		}
		if _, ok := resolutionErrors[taskID]; ok {
			return TaskDependencies{}, false
		}
		if err, ok := documentErrors[taskID]; ok {
			resolutionErrors[taskID] = err
			return TaskDependencies{}, false
		}
		value, err := TaskDependenciesOf(board.Entries[taskID], board, documents)
		if err != nil {
			resolutionErrors[taskID] = err
			return TaskDependencies{}, false
		}
		resolved[taskID] = value
		return value, true
	}

	var roots []string
	if selected != nil {
		for taskID := range selected {
			roots = append(roots, taskID)
		}
	} else {
		for taskID := range board.Entries {
			roots = append(roots, taskID)
		}
	}
	sort.Strings(roots)
	if len(skipStates) > 0 {
		filtered := roots[:0]
		for _, taskID := range roots {
			entry, ok := board.Entries[taskID]
			if !ok {
				continue
			}
			_, skipped := skipStates[entry.State]
			_, must := selected[taskID]
			if !skipped || (selected != nil && must) {
				filtered = append(filtered, taskID)
			}
		}
		roots = filtered
	}
	mustCheck := map[string]struct{}{}
	for _, taskID := range roots {
		mustCheck[taskID] = struct{}{}
	}
	var problems []Problem
	reportedErrors := map[string]struct{}{}
	reportedCycles := map[string]struct{}{}
	var visiting []string
	visited := map[string]struct{}{}

	var visit func(string)
	visit = func(taskID string) {
		entry, ok := board.Entries[taskID]
		if ok {
			if _, skipped := skipStates[entry.State]; skipped {
				if _, must := mustCheck[taskID]; !must {
					visited[taskID] = struct{}{}
					return
				}
			}
		}
		if _, ok := visited[taskID]; ok {
			return
		}
		for i, current := range visiting {
			if current == taskID {
				cycle := append(append([]string{}, visiting[i:]...), taskID)
				key := strings.Join(cycle, " -> ")
				if _, seen := reportedCycles[key]; !seen {
					reportedCycles[key] = struct{}{}
					source := board.Entries[visiting[len(visiting)-1]]
					problems = append(problems, Problem{
						Path:    source.Path,
						Message: t("board.dependency_cycle", key),
					})
				}
				return
			}
		}
		visiting = append(visiting, taskID)
		deps, ok := resolve(taskID)
		if !ok {
			if _, seen := reportedErrors[taskID]; !seen {
				reportedErrors[taskID] = struct{}{}
				problems = append(problems, Problem{
					Path:    board.Entries[taskID].Path,
					Message: resolutionErrors[taskID].Error(),
				})
			}
		} else {
			for _, prerequisiteID := range deps.ExpandedTaskIDs {
				visit(prerequisiteID)
			}
		}
		visiting = visiting[:len(visiting)-1]
		visited[taskID] = struct{}{}
	}

	for _, root := range roots {
		if _, ok := board.Entries[root]; ok {
			visit(root)
		}
	}
	return problems
}

// CheckBoard 校验入口与依赖图, 不含存活探测. 返回退出码与 stdout 汇总行.
func CheckBoard(root string, taskIDs []string, includeAll bool) (code int, stdout string, stderr []string, err error) {
	skipStates := map[string]struct{}{}
	if !includeAll {
		skipStates = deferredCheckStates
	}
	var board Board
	if len(taskIDs) > 0 {
		board, err = ScanTargets(root, taskIDs)
	} else {
		board, err = Scan(root)
	}
	if err != nil {
		return 1, "", nil, err
	}
	var allProblems []Problem
	if len(taskIDs) > 0 {
		allProblems = append(allProblems, board.Problems...)
	} else {
		for _, problem := range board.Problems {
			if problemInCheckScope(root, problem, includeAll) {
				allProblems = append(allProblems, problem)
			}
		}
	}
	dependencyBoard := board
	if len(taskIDs) > 0 {
		dependencyBoard, err = Scan(root)
		if err != nil {
			return 1, "", nil, err
		}
	}
	var selected map[string]struct{}
	if len(taskIDs) > 0 {
		selected = map[string]struct{}{}
		for id := range board.Entries {
			selected[id] = struct{}{}
		}
	}
	allProblems = append(allProblems, dependencyProblems(dependencyBoard, selected, skipStates)...)
	allProblems = append(allProblems, contractProblems(board)...)
	checked := 0
	if len(taskIDs) > 0 || includeAll {
		checked = len(board.Entries)
	} else {
		for _, entry := range board.Entries {
			if _, skip := deferredCheckStates[entry.State]; !skip {
				checked++
			}
		}
	}
	for _, problem := range allProblems {
		stderr = append(stderr, t("board.invalid", problem.Message))
	}
	if len(allProblems) > 0 {
		return 1, t(
			"board.checked_valid_invalid", itoa(checked), itoa(len(allProblems)),
		) + "\n", stderr, nil
	}
	return 0, t("board.ok_tasks", itoa(checked)) + "\n", nil, nil
}
