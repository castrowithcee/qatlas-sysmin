package cli

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"
)

var errNoTerminal = errors.New("no terminal is attached")

// readSecretTerminal prompts on the controlling terminal and reads one line back without echoing it. Like
// the vault's passphrase prompt it needs a person: at least one standard stream must be a terminal, which
// keeps a headless session from waiting for an answer nobody gives.
func readSecretTerminal(prompt string) ([]byte, error) {
	if !isTerminal(os.Stdin) && !isTerminal(os.Stdout) && !isTerminal(os.Stderr) {
		return nil, errNoTerminal
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err == nil {
		defer tty.Close()
	} else if isTerminal(os.Stdin) {
		tty = os.Stdin
	} else {
		return nil, errNoTerminal
	}
	if _, err := fmt.Fprint(tty, prompt); err != nil {
		return nil, errNoTerminal
	}
	secret, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty)
	if err != nil {
		return nil, errNoTerminal
	}
	return secret, nil
}
