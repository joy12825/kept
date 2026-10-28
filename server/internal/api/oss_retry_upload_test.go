//go:build !ee

package api

import "testing"

// checkLinkUploadRetry: the open-source build has no upload links (v1.13).
func checkLinkUploadRetry(*testing.T, *testEnv, *client, *client) {}
