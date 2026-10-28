//go:build !ee

package api

// EEDeps is empty in the open-source build: it has no paid modules (v1.13).
type EEDeps struct{}
