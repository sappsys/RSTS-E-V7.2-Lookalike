// Command rsts is the RSTS/E V7.2 lookalike timesharing CLI.
package main

import (
	"os"

	"rsts"
)

func main() {
	os.Exit(rsts.Main(os.Args[1:]))
}
