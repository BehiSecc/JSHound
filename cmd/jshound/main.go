package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	fmt.Fprintf(os.Stderr, "JSHound %s: implementation in progress\n", version)
}
