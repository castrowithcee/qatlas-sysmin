package execute

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// shell is the only program that interprets a command line.
const shell = "/bin/sh"

func needDir(st config.ResolvedStep) error {
	if st.Dir == "" {
		return errors.New("step has no project directory")
	}
	return nil
}

// composeCommand builds docker compose with the project's files in their fixed order, the project name when
// set, and then the subcommand words and the services.
func composeCommand(st config.ResolvedStep, verb ...string) (Command, error) {
	if err := needDir(st); err != nil {
		return Command{}, err
	}
	args := []string{"compose"}
	for _, f := range st.ComposeFiles {
		args = append(args, "-f", f)
	}
	if st.ComposeProject != "" {
		args = append(args, "-p", st.ComposeProject)
	}
	args = append(args, verb...)
	for _, svc := range st.Services {
		if svc == "" || strings.HasPrefix(svc, "-") {
			return Command{}, fmt.Errorf("service %q cannot be passed as an argument", svc)
		}
		args = append(args, svc)
	}
	return Command{Name: "docker", Args: args, Dir: st.Dir}, nil
}

func runCompose(verb ...string) func(context.Context, *stepRun, config.ResolvedStep) error {
	return func(ctx context.Context, s *stepRun, st config.ResolvedStep) error {
		c, err := composeCommand(st, verb...)
		if err != nil {
			return err
		}
		_, err = s.run(ctx, c)
		return err
	}
}

func describeCompose(verb ...string) func(config.ResolvedStep) ([]string, error) {
	return func(st config.ResolvedStep) ([]string, error) {
		c, err := composeCommand(st, verb...)
		if err != nil {
			return nil, err
		}
		return []string{displayCommand(c) + " (in " + st.Dir + ")"}, nil
	}
}

// gitCommands returns the calls of a git-checkout step in order. The tag is only ever used as part of a full
// ref name, so it cannot be read as an option.
func gitCommands(st config.ResolvedStep, env []string) (status, fetch, checkout Command, err error) {
	if err = needDir(st); err != nil {
		return
	}
	if st.Tag == "" || strings.HasPrefix(st.Tag, "-") {
		err = fmt.Errorf("tag %q cannot be checked out", st.Tag)
		return
	}
	// Never ask for credentials on a terminal; a missing credential must fail instead of hanging.
	env = append([]string{"GIT_TERMINAL_PROMPT=0"}, env...)
	mk := func(args ...string) Command { return Command{Name: "git", Args: args, Dir: st.Dir, Env: env} }
	status = mk("status", "--porcelain", "--untracked-files=all")
	fetch = mk("fetch", "--tags", "--force", "origin")
	checkout = mk("checkout", "--detach", "refs/tags/"+st.Tag, "--")
	return
}

func runGitCheckout(ctx context.Context, s *stepRun, st config.ResolvedStep) error {
	status, fetch, checkout, err := gitCommands(st, s.e.GitEnv)
	if err != nil {
		return err
	}
	res, err := s.run(ctx, status)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(res.Output)) != "" {
		return errors.New("refusing to check out: the working tree has local changes")
	}
	if _, err := s.run(ctx, fetch); err != nil {
		return err
	}
	_, err = s.run(ctx, checkout)
	return err
}

func describeGitCheckout(st config.ResolvedStep) ([]string, error) {
	status, fetch, checkout, err := gitCommands(st, nil)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range []Command{status, fetch, checkout} {
		out = append(out, displayCommand(c)+" (in "+st.Dir+")")
	}
	return out, nil
}

func shellCommand(st config.ResolvedStep) (Command, error) {
	if err := needDir(st); err != nil {
		return Command{}, err
	}
	if strings.TrimSpace(st.Shell) == "" {
		return Command{}, errors.New("command step has no command line")
	}
	return Command{Name: shell, Args: []string{"-c", st.Shell}, Dir: st.Dir}, nil
}

func runCommand(ctx context.Context, s *stepRun, st config.ResolvedStep) error {
	c, err := shellCommand(st)
	if err != nil {
		return err
	}
	_, err = s.run(ctx, c)
	return err
}

func describeCommand(st config.ResolvedStep) ([]string, error) {
	c, err := shellCommand(st)
	if err != nil {
		return nil, err
	}
	return []string{displayCommand(c) + " (in " + st.Dir + ")"}, nil
}

// displayCommand renders a command for the log and the dry run; the quoting is for reading only.
func displayCommand(c Command) string {
	words := make([]string, 0, len(c.Args)+1)
	words = append(words, quoteWord(c.Name))
	for _, a := range c.Args {
		words = append(words, quoteWord(a))
	}
	return strings.Join(words, " ")
}

func quoteWord(w string) string {
	if w != "" && strings.Trim(w, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return w
	}
	return "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
}
