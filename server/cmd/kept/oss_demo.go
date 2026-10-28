//go:build !ee

package main

import (
	"kept/server/internal/demo"
	"kept/server/internal/document"
	"kept/server/internal/drawer"
	"kept/server/internal/files"
	"kept/server/internal/store"
	"kept/server/internal/taxonomy"
)

// demoEE: the open-source build has no paid modules (v1.13).
func demoEE(*store.Store, *files.Store, *document.Service, *taxonomy.Service, *drawer.Service) demo.EEDeps {
	return demo.EEDeps{}
}
