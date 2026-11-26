package taskindex

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/dualface/kander/v2/api"
)

// Position is the exclusive ordering key of the last row on a page.
type Position struct {
	StateRank int
	Time      string
	TaskID    string
}

func (r Row) Position() Position {
	return Position{r.StateRank, r.Summary.Time, r.Summary.TaskID}
}

func (s *Store) List(ctx context.Context, query api.TaskListOptions, after *Position) ([]Row, bool, error) {
	var conditions []string
	var args []any
	for _, field := range []struct{ column, value string }{
		{"state", query.State}, {"assignee", query.Assignee}, {"submitter", query.Submitter}, {"task_group", query.Group}, {"decision_mode", string(query.DecisionMode)},
	} {
		if field.value != "" {
			conditions = append(conditions, field.column+" = ?")
			args = append(args, field.value)
		}
	}
	if query.Search != "" {
		if utf8.RuneCountInString(query.Search) >= 3 {
			conditions = append(conditions, "rowid IN (SELECT rowid FROM tasks_fts WHERE tasks_fts MATCH ?)")
			// Quote the entire search as a literal phrase, not an FTS expression.
			args = append(args, `"`+strings.ReplaceAll(query.Search, `"`, `""`)+`"`)
		} else {
			conditions = append(conditions, `title LIKE ? ESCAPE '\'`)
			escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(query.Search)
			args = append(args, "%"+escaped+"%")
		}
	}
	if after != nil {
		conditions = append(conditions, "(state_rank > ? OR (state_rank = ? AND (display_time, task_id) < (?, ?)))")
		args = append(args, after.StateRank, after.StateRank, after.Time, after.TaskID)
	}
	statement := "SELECT " + columns + " FROM tasks"
	if len(conditions) > 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += " ORDER BY state_rank, display_time DESC, task_id DESC"
	if query.Limit > 0 {
		statement += " LIMIT ?"
		args = append(args, query.Limit+1)
	}
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		row, err := scanRow(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if query.Limit > 0 && len(out) > query.Limit {
		return out[:query.Limit], true, nil
	}
	return out, false, nil
}
