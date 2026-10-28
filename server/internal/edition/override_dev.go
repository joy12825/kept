//go:build kept_dev

package edition

import "os"

// DevOverride, in development builds (-tags kept_dev), is the edition
// KEPT_EDITION names (free, pro or team), for demos and tests.
func DevOverride() *Edition {
	e, err := Parse(os.Getenv("KEPT_EDITION"))
	if os.Getenv("KEPT_EDITION") == "" || err != nil {
		return nil
	}
	return &e
}
