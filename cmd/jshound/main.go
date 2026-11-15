package main

import (
	"context"
	"os"
	"runtime/debug"

	"github.com/BehiSecc/JSHound/internal/app"
)

// version can be set at link time with -ldflags "-X main.version=v1.2.3".
// When it is empty, the module version that the Go toolchain embeds is used.
var version string

func main() {
	info, _ := debug.ReadBuildInfo()
	os.Exit(app.Run(context.Background(), os.Args[1:], app.Config{Version: resolveVersion(version, info)}))
}

// resolveVersion returns the link-time version if set, otherwise the main
// module's version from the build info: a release tag such as v1.2.3 for
// "go install ...@v1.2.3", or a pseudo-version for a build of an untagged
// commit. It returns "dev" only when the build has no version information.
func resolveVersion(linked string, info *debug.BuildInfo) string {
	if linked != "" {
		return linked
	}
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
