//go:build !ee

package api

// ssoName: the open-source build has no single sign-on (v1.13).
func (s *Server) ssoName() (string, bool) { return "", false }
