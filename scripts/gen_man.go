//go:build ignore

// gen_man writes the manpage qsysmin.1 of the command tree into a directory. Run it from the repository root:
//
//	SOURCE_DATE_EPOCH=<seconds> go run -ldflags "-X github.com/castrowithcee/qatlas-sysmin/internal/cli.version=<tag>" scripts/gen_man.go <dir>
//
// The date in the page header comes from SOURCE_DATE_EPOCH, so the same source gives the same page.
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/cli"
)

func main() {
	if len(os.Args) != 2 {
		fail("usage: go run scripts/gen_man.go <output directory>")
	}
	epoch, err := strconv.ParseInt(os.Getenv("SOURCE_DATE_EPOCH"), 10, 64)
	if err != nil {
		fail("SOURCE_DATE_EPOCH must be set to a number of seconds")
	}
	if err := cli.WriteManpage(os.Args[1], time.Unix(epoch, 0)); err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "gen_man: "+msg)
	os.Exit(1)
}
