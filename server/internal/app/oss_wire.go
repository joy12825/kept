//go:build !ee

package app

// wireEE: the open-source build has no paid modules (v1.13).
func wireEE(eeWiring) eeParts { return eeParts{} }
