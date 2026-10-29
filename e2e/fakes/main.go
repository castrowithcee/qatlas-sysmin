//go:build e2efakes

// Command fakes provides the helper services of the end-to-end environment: a GitHub releases fake with
// release downloads, a git smart-HTTP server, a webhook receiver and the application of the demo images. It
// is built only with -tags e2efakes, so the normal build, vet and test runs never see it.
package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const usage = "usage: fakes releases|git|webhook|demoapp|htpasswd <user>|sign <body-file> (secret on stdin)"

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
	case "demoapp":
		err = runDemoApp()
	case "htpasswd":
		err = runHtpasswd(os.Args[2:])
	case "sign":
		err = runSign(os.Args[2:])
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

// runSign prints the HMAC-SHA256 signature of a request body as sha256=<hex>, the form of the
// X-Hub-Signature-256 header; the secret is the first line of stdin so it never appears in a process list.
func runSign(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: sign <body-file> (secret on stdin)")
	}
	secret, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && secret == "" {
		return fmt.Errorf("no secret on stdin")
	}
	body, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(strings.TrimRight(secret, "\r\n")))
	mac.Write(body)
	fmt.Printf("sha256=%s\n", hex.EncodeToString(mac.Sum(nil)))
	return nil
}
