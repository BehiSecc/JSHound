package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/BehiSecc/JSHound/internal/app"
)

// version can be set at link time with -ldflags "-X main.version=v1.2.3".
// When it is empty, the module version that the Go toolchain embeds is used.
var version string

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-interrupts
		// Cancel requests and Kingfisher so their temporary files are removed,
		// then exit if analysis is still running. A second signal exits at once.
		cancel()
		signal.Stop(interrupts)
		time.Sleep(2 * time.Second)
		os.Exit(130)
	}()

	info, _ := debug.ReadBuildInfo()
	code := app.Run(ctx, os.Args[1:], app.Config{Version: resolveVersion(version, info)})
	if ctx.Err() != nil {
		code = 130 // interrupted
	}
	cancel()
	os.Exit(code)
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
