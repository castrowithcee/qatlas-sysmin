//go:build e2efakes

// Command fakes provides the helper services of the end-to-end environment: a GitHub releases fake with
// release downloads, a git smart-HTTP server and a webhook receiver. It is built only with -tags e2efakes,
// so the normal build, vet and test runs never see it.
package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

const usage = "usage: fakes releases|git|webhook|htpasswd <user> <password-from-stdin>"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "releases":
		err = runReleases()
	case "git":
		err = runGit()
	case "webhook":
		err = runWebhook()
	case "htpasswd":
		err = runHtpasswd(os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakes:", err)
		os.Exit(1)
	}
}

// runHtpasswd prints an htpasswd line with a bcrypt hash; the password is read from stdin so it never
// appears in a process list.
func runHtpasswd(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: htpasswd <user> (password on stdin)")
	}
	var pw string
	if _, err := fmt.Fscanln(os.Stdin, &pw); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	fmt.Printf("%s:%s\n", args[0], hash)
	return nil
}
