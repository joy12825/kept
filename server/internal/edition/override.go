//go:build !kept_dev

package edition

// DevOverride is nil in release builds: only a license or the trial sets
// the edition.
func DevOverride() *Edition { return nil }
