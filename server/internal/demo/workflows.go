package demo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// loadWorkflows adds two sample workflows to the family workspace: mailed
// utility bills are typed and tagged while being consumed, and a disabled
// reminder emails contracts a month before they are due.
func loadWorkflows(ctx context.Context, d Deps, scope tenant.Scope, email string) (int, error) {
	ws := scope.WorkspaceID()
	q := sqlc.New(d.DB)
	id := func(query string, args ...any) int64 {
		var v int64
		if err := d.DB.QueryRowContext(ctx, query, args...).Scan(&v); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0
		}
		return v
	}
	bill := id(`SELECT id FROM document_types WHERE workspace_id = ? AND name = '账单'`, ws)
	contract := id(`SELECT id FROM document_types WHERE workspace_id = ? AND name = '合同'`, ws)
	utility := id(`SELECT id FROM tags WHERE workspace_id = ? AND name = '水电'`, ws)
	due := id(`SELECT id FROM custom_fields WHERE workspace_id = ? AND key = 'due_date'`, ws)
	if email == "" {
		email = "family@example.com"
	}
	type wf struct {
		name                         string
		enabled                      bool
		triggers, conditions, action any
	}
	flows := []wf{
		{"邮件收到的水电账单", true,
			[]any{map[string]any{"type": "consumption"}},
			map[string]any{"sources": []string{"mail"}, "filter": map[string]any{"any": []any{map[string]any{"text": "电费"}, map[string]any{"text": "水费"}, map[string]any{"text": "燃气"}}}},
			[]any{map[string]any{"type": "assign", "typeId": bill, "tagIds": []int64{utility}}}},
		{"合同到期前一个月提醒", false,
			[]any{map[string]any{"type": "scheduled", "field": "cf:" + strconv.FormatInt(due, 10), "offsetDays": -30}},
			map[string]any{"filter": map[string]any{"field": "type", "op": "in", "value": []int64{contract}}},
			[]any{map[string]any{"type": "email", "to": []string{email}, "subject": "合同快到期了：{标题}", "body": "“{标题}”（{往来方}）还有一个月到期。\n{链接}", "attach": true}}},
	}
	added := 0
	for _, f := range flows {
		if bill == 0 || contract == 0 || utility == 0 || due == 0 {
			break
		}
		if id(`SELECT id FROM workflows WHERE workspace_id = ? AND name = ?`, ws, f.name) != 0 {
			continue
		}
		t, _ := json.Marshal(f.triggers)
		c, _ := json.Marshal(f.conditions)
		a, _ := json.Marshal(f.action)
		enabled := int64(0)
		if f.enabled {
			enabled = 1
		}
		if _, err := q.InsertWorkflow(ctx, sqlc.InsertWorkflowParams{WorkspaceID: ws, Name: f.name, Enabled: enabled,
			Triggers: string(t), Conditions: string(c), Actions: string(a), Now: time.Now().UnixMilli()}); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}
