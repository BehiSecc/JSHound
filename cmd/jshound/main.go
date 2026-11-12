package main

import (
	"context"
	"os"

	"github.com/BehiSecc/JSHound/internal/app"
)

var version = "dev"

func main() {
	os.Exit(app.Run(context.Background(), os.Args[1:], app.Config{Version: version}))
}
