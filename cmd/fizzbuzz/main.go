package main

import (
	"context"
	"fmt"
	"os"
)

func run(ctx context.Context) error {
	_ = ctx

	return nil
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
}
