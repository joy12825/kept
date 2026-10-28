//go:build !ee

package api

import (
	"context"

	"kept/server/internal/ingest"
)

// testEE: the open-source build has no paid modules (v1.13).
func testEE(eeTestWiring) EEDeps { return EEDeps{} }

// eeBuild: whether this build has the paid modules (v1.13).
const eeBuild = false

// workflows: none in the open-source build.
func (EEDeps) workflows() (ingest.Workflows, func(ws int64, ids []int64)) { return nil, nil }

// added: nothing hears about new documents in the open-source build.
func (EEDeps) added() func(ctx context.Context, ws, id int64) { return nil }
