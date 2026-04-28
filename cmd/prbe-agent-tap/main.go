package main

import (
	"context"
	"os"
)

func main() {
	os.Exit(dispatch(context.Background(), os.Args, os.Stdout, os.Stderr))
}
