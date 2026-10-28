//go:build !ee

package demo

import (
	"context"

	"kept/server/internal/tenant"
)

// EEDeps is empty in the open-source build: it has no paid modules (v1.13).
type EEDeps struct{}

// The open-source build has no scenes, lab reports or AI helpers (v1.13).

func demoLab(context.Context, Deps, tenant.Scope) (int, error)                { return 0, nil }
func demoFindings(context.Context, Deps, tenant.Scope) (int, error)           { return 0, nil }
func demoScenes(context.Context, Deps, tenant.Scope) (int, error)             { return 0, nil }
func loadQADemo(context.Context, Deps, tenant.Scope, int64) (int, error)      { return 0, nil }
func loadOrganizeDemo(context.Context, Deps, tenant.Scope) (int, error)       { return 0, nil }
func loadStewardDemo(context.Context, Deps, tenant.Scope) (int, error)        { return 0, nil }
func loadLibraryDemo(context.Context, Deps, tenant.Scope, int64) (int, error) { return 0, nil }
