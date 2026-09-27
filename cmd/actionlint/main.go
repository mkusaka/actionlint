package main

import (
	"os"

	"github.com/mkusaka/actionlint"

	_ "time/tzdata"
)

func main() {
	cmd := actionlint.Command{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	os.Exit(cmd.Main(os.Args))
}
