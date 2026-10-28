//go:build !ee

package demo

import (
	"context"

	"kept/server/internal/tenant"
)

// shareAcross: the open-source build shares no drawers or collections
// across spaces (v1.13, v1.13.2).
func shareAcross(context.Context, Deps, tenant.Scope, int64, int64) error { return nil }
