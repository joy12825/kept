// Package version holds build information injected at link time:
//
//	go build -ldflags "-X kept/server/internal/version.Version=v0.1.0 ..."
package version

var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)
