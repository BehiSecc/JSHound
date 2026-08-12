package main

import (
	"context"
	"os"

	"github.com/jshound/jshound/internal/app"
)

var version = "dev"

func main() {
	os.Exit(app.Run(context.Background(), os.Args[1:], app.Config{Version: version}))
}
