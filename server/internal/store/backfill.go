package store

import (
	"context"
	"fmt"

	"kept/server/internal/pinyin"
	"kept/server/internal/store/sqlc"
)

// backfillTitleSort computes documents.title_sort for rows written before
// migration 0009 added it (SQL cannot compute pinyin). It only has work to
// do on the first start after the upgrade.
func backfillTitleSort(ctx context.Context, q *sqlc.Queries) error {
	for {
		rows, err := q.ListDocumentsWithoutTitleSort(ctx)
		if err != nil {
			return fmt.Errorf("backfill title sort keys: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		for _, r := range rows {
			if err := q.SetDocumentTitleSort(ctx, sqlc.SetDocumentTitleSortParams{TitleSort: pinyin.TitleKey(r.Title), ID: r.ID}); err != nil {
				return fmt.Errorf("backfill title sort keys: %w", err)
			}
		}
	}
}
