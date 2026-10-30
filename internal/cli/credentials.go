package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

// maxSecret bounds a secret read from standard input; it keeps the request below the size limit even when
// the encoding of the secret grows.
const maxSecret = 16 << 10

// oneNamed accepts exactly one argument and never repeats it, since a mistaken argument may be a secret.
func oneNamed(verb, what string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return newSyntaxError(fmt.Errorf("%s takes exactly one %s name, got %d arguments; a secret is never given as an argument", verb, what, len(args)))
		}
		return nil
	}
}

// noArgs accepts no argument and never repeats one.
func noArgs(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return newSyntaxError(fmt.Errorf("unexpected arguments (%d)", len(args)))
	}
	return nil
}

func newCredentialCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credential",
		Short: "Store, list and remove vault credentials",
		Long: "A credential is a secret in the vault that projects and notification channels refer to by name. These\n" +
			"commands change the vault through the service and never ask for the vault passphrase. A secret is\n" +
			"never an argument or an environment variable: it is typed at a hidden prompt or read from standard\n" +
			"input, and no command prints a secret value. Replacing a credential gives it a new id; what uses it\n" +
			"counts as open until a person approves it in the terminal interface (sysmin tui), and keeps\n" +
			"running with the previous value until then. The vault must be unlocked.",
		Args: noArgs,
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newCredentialSetCommand(opts), newCredentialListCommand(opts), newCredentialRemoveCommand(opts))
	return cmd
}

// credentialError turns the codes of a credential request into messages that say what to do.
func credentialError(what string, err error) error {
	var ce *control.Error
	if errors.As(err, &ce) {
		switch ce.Code {
		case control.CodeLocked:
			return fmt.Errorf("%s: the vault is locked; unlock it in the terminal interface first", what)
		case control.CodeVaultAbsent:
			return fmt.Errorf("%s: there is no vault yet", what)
		case control.CodeNotFound:
			return fmt.Errorf("%s: the credential does not exist", what)
		case control.CodeExists:
			return fmt.Errorf("%s: the credential was created meanwhile; repeat the command", what)
		case control.CodeKindMismatch:
			return fmt.Errorf("%s: a credential of this name exists with another kind; remove it first or use its kind", what)
		case control.CodeBadRequest:
			return fmt.Errorf("%s: the service did not accept the request; check the credential name", what)
		case control.CodeTooLarge:
			return fmt.Errorf("%s: the request is too large for the service", what)
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

func kindNames() string {
	names := make([]string, len(control.CredentialKinds))
	for i, k := range control.CredentialKinds {
		names[i] = k.Name
	}
	return strings.Join(names, ", ")
}

func flagOf(field string) string { return strings.ReplaceAll(field, "_", "-") }

// readSecretInput returns the secret of kind k. With fromStdin the whole standard input is the secret, minus
// one trailing line end, so a multi-line key works; otherwise the secret is typed twice at a hidden prompt.
// The caller wipes the result.
func readSecretInput(c *cobra.Command, opts *Options, k control.CredentialKind, name string, fromStdin bool) ([]byte, error) {
	if fromStdin {
		buf := make([]byte, maxSecret+1)
		n, err := io.ReadFull(c.InOrStdin(), buf)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			clear(buf)
			return nil, fmt.Errorf("cannot read standard input: %w", err)
		}
		if n > maxSecret {
			clear(buf)
			return nil, &UsageError{fmt.Errorf("the secret on standard input is longer than %d bytes", maxSecret)}
		}
		secret := bytes.TrimSuffix(bytes.TrimSuffix(buf[:n], []byte("\n")), []byte("\r"))
		if len(secret) == 0 {
			clear(buf)
			return nil, &UsageError{errors.New("standard input is empty; nothing was stored")}
		}
		return secret, nil
	}
	if k.Multiline {
		return nil, &UsageError{fmt.Errorf("the %s of a %s credential spans lines and cannot be typed; give it on standard input with --from-stdin", k.Secret, k.Name)}
	}
	ask := opts.readSecret
	if ask == nil {
		ask = readSecretTerminal
	}
	noTTY := func(err error) error {
		if errors.Is(err, errNoTerminal) {
			return &UsageError{errors.New("no terminal is attached to type the secret; give it on standard input with --from-stdin")}
		}
		return err
	}
	first, err := ask(fmt.Sprintf("Value of %s for %s (input is hidden): ", k.Secret, name))
	if err != nil {
		return nil, noTTY(err)
	}
	second, err := ask("Repeat the value: ")
	if err != nil {
		clear(first)
		return nil, noTTY(err)
	}
	same := bytes.Equal(first, second)
	clear(second)
	if !same || len(first) == 0 {
		clear(first)
		return nil, &UsageError{errors.New("the entries are empty or differ; nothing was stored")}
	}
	return first, nil
}

func newCredentialSetCommand(opts *Options) *cobra.Command {
	var socket, kind string
	var fromStdin bool
	plain := map[string]*string{}
	cmd := &cobra.Command{
		Use:   "set NAME --kind KIND",
		Short: "Store a credential, or replace the one of that name",
		Long: "set stores a new credential, or replaces the fields of the credential of that name under a new id.\n" +
			"The kind is one of: " + kindNames() + ". The parts that are not secret are options: --user for registry,\n" +
			"git-https and smtp, --host-key for ssh-key. The secret is the token of registry and git-https, the\n" +
			"password of smtp, the private key of ssh-key and the value of secret. It is typed twice at a hidden\n" +
			"prompt, or with --from-stdin the whole of standard input is the secret, minus one trailing line end;\n" +
			"a private key can only be given that way. A secret is never an argument or an environment\n" +
			"variable. Without an admin session what uses a replaced credential becomes pending approval: it\n" +
			"keeps running with the previous value until a person approves it in the terminal interface.",
		Example: "  sysmin credential set demo-registry --kind registry --user demo-user\n" +
			"  sysmin credential set demo-deploy-key --kind ssh-key --host-key \"$(cat host.pub)\" --from-stdin < key",
		Args: oneNamed("set", "credential"),
		RunE: func(c *cobra.Command, args []string) error {
			name := args[0]
			if kind == "" {
				return newSyntaxError(errors.New("--kind is required; kinds: " + kindNames()))
			}
			k, ok := control.KindOf(kind)
			if !ok {
				return newSyntaxError(fmt.Errorf("unknown credential kind %q; kinds: %s", kind, kindNames()))
			}
			fields := map[string]string{}
			for field, v := range plain {
				given := c.Flags().Changed(flagOf(field))
				applies := false
				for _, p := range k.Plain {
					applies = applies || p == field
				}
				switch {
				case given && !applies:
					return newSyntaxError(fmt.Errorf("--%s does not apply to a %s credential", flagOf(field), k.Name))
				case !given && applies:
					return newSyntaxError(fmt.Errorf("a %s credential needs --%s", k.Name, flagOf(field)))
				case given:
					fields[field] = *v
				}
			}
			secret, err := readSecretInput(c, opts, k, name, fromStdin)
			if err != nil {
				return err
			}
			defer clear(secret)
			opts.Redactor.Add(string(secret))
			fields[k.Secret] = string(secret)
			params := control.CredentialSetParams{Name: name, Kind: k.Name, Fields: fields}
			var res control.CredentialResult
			err = withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.SetCredential(params)
				return err
			})
			if err != nil {
				return finishWith(c, opts, "credential set", true, err, credentialError)
			}
			return credentialResult(c, opts, res)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&kind, "kind", "", "kind of the credential: "+kindNames())
	for _, k := range control.CredentialKinds {
		for _, field := range k.Plain {
			if plain[field] == nil {
				plain[field] = new(string)
				fl.StringVar(plain[field], flagOf(field), "", "the "+strings.ReplaceAll(field, "_", " ")+" (not secret)")
			}
		}
	}
	fl.BoolVar(&fromStdin, "from-stdin", false, "read the secret from standard input instead of a hidden prompt")
	fl.StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func joined(list []string) string { return strings.Join(list, ", ") }

// credentialResult prints the outcome of a set or remove and what it means.
func credentialResult(c *cobra.Command, opts *Options, res control.CredentialResult) error {
	var message string
	switch {
	case res.State == control.StatePending && res.Action == "removed":
		message = "removed; the removal leaves " + joined(res.Pending) + " open: a person must approve it in the terminal interface (sysmin tui)"
	case res.State == control.StatePending && len(res.Pending) == 0:
		message = "stored; whether something now waits for an approval because of it could not be determined: see sysmin approvals list"
	case res.State == control.StatePending:
		message = "stored; not approved yet: " + joined(res.Pending) + " use it and a person must approve the change in the terminal interface (sysmin tui) before it takes effect; until then they keep the previous value"
	case res.Action == "removed":
		message = "removed"
	case len(res.Approved) > 0:
		message = "stored and approved for " + joined(res.Approved)
	default:
		message = "stored; nothing that needs an approval uses it"
	}
	fields := []output.Field{
		{Name: "credential", Value: res.Name},
		{Name: "action", Value: res.Action},
		{Name: "id", Value: res.ID},
		{Name: "state", Value: res.State},
		{Name: "pending", Value: joined(res.Pending)},
		{Name: "approved", Value: joined(res.Approved)},
		{Name: "message", Value: message},
	}
	return emit(c, opts, output.Object{Fields: fields})
}

func newCredentialListCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the credentials and what uses them",
		Long: "list shows the name, kind and id of every credential and the projects and channels that use it, in\n" +
			"the current configuration or the approved state. It never shows a value. The vault must be unlocked.",
		Args: noArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var list []control.CredentialSummary
			err := withService(socket, func(cl *client.Client) (err error) {
				list, err = cl.Credentials()
				return err
			})
			if err != nil {
				return credentialError("credential list", err)
			}
			result := output.Collection{Columns: []string{"name", "kind", "id", "used_by"}}
			for _, cr := range list {
				uses := make([]string, 0, len(cr.Uses))
				for _, u := range cr.Uses {
					var where []string
					if u.Current {
						where = append(where, "current")
					}
					if u.Approved {
						where = append(where, "approved")
					}
					uses = append(uses, fmt.Sprintf("%s %s (%s)", u.Kind, u.Name, strings.Join(where, ", ")))
				}
				result.Rows = append(result.Rows, output.Row{"name": cr.Name, "kind": cr.Kind, "id": cr.ID, "used_by": strings.Join(uses, "; ")})
			}
			return emit(c, opts, result)
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newCredentialRemoveCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "remove NAME",
		Short: "Remove a credential",
		Long: "remove deletes a credential that no project or channel uses. One that is used, in the current\n" +
			"configuration or the approved state, is refused with the list of its uses: remove the uses first, or\n" +
			"have a person remove it in an admin session, which leaves what used it pending approval.",
		Args: oneNamed("remove", "credential"),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.CredentialResult
			err := withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.RemoveCredential(args[0])
				return err
			})
			var ce *control.Error
			if errors.As(err, &ce) && ce.Code == control.CodeInUse {
				if eerr := emit(c, opts, problemsResult(ce.Problems)); eerr != nil {
					return eerr
				}
				return fmt.Errorf("credential remove: the credential is in use by %d projects or channels; nothing was removed; remove the uses first, or have a person remove it in an admin session", len(ce.Problems))
			}
			if err != nil {
				return credentialError("credential remove", err)
			}
			return credentialResult(c, opts, res)
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}
