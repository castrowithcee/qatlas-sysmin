package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

func newNotifyCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Manage and test notification channels",
		Args:  noArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newChannelCommand(opts), newNotifyTestCommand(opts))
	return cmd
}

func newChannelCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Add, list and remove notification channels",
		Long: "A channel is a place a notification goes to: a webhook, an email server, a Telegram chat or a Slack\n" +
			"hook. The secret a channel needs is a vault credential that the channel names; see 'sysmin\n" +
			"credential'. These commands change the channels through the service and never ask for the vault\n" +
			"passphrase: a change is stored, but the channels count as open until a person approves them as a\n" +
			"whole in the terminal interface (sysmin tui), and the approved channels stay in effect until then.\n" +
			"The vault must be unlocked for a change.",
		Args: noArgs,
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newChannelAddCommand(opts), newChannelListCommand(opts), newChannelRemoveCommand(opts))
	return cmd
}

// channelError turns the codes of a channel request into messages that say what to do.
func channelError(what string, err error) error {
	var ce *control.Error
	if errors.As(err, &ce) {
		switch ce.Code {
		case control.CodeLocked:
			return fmt.Errorf("%s: the vault is locked; unlock it in the terminal interface first", what)
		case control.CodeVaultAbsent:
			return fmt.Errorf("%s: there is no vault yet", what)
		case control.CodeNotFound:
			return fmt.Errorf("%s: the channel does not exist", what)
		case control.CodeExists:
			return fmt.Errorf("%s: the channel exists already; remove it first to change it", what)
		case control.CodeBadRequest:
			return fmt.Errorf("%s: the service did not accept the request; check the channel name", what)
		case control.CodeTooLarge:
			return fmt.Errorf("%s: the request is too large for the service", what)
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

// channelResult prints the outcome of a change of the channels and what it means.
func channelResult(c *cobra.Command, opts *Options, name, action string, res control.ChangeResult) error {
	message := "stored; not approved yet: a person must approve the channels in the terminal interface (sysmin tui) before the change takes effect; until then the approved channels stay in effect"
	switch {
	case res.State == control.StateApproved && action == "removed":
		message = "removed; no approval is open for the channels"
	case res.State == control.StateApproved:
		message = "stored and approved"
	case action == "removed":
		message = "removed; the removal is not approved yet: a person must approve the channels in the terminal interface (sysmin tui), and until then the approved channels stay in effect"
	}
	return emit(c, opts, output.Object{Fields: []output.Field{
		{Name: "channel", Value: name},
		{Name: "action", Value: action},
		{Name: "approval", Value: res.Project},
		{Name: "state", Value: res.State},
		{Name: "revision", Value: res.Revision},
		{Name: "message", Value: message},
	}})
}

func newChannelAddCommand(opts *Options) *cobra.Command {
	var socket string
	var p control.ChannelParams
	cmd := &cobra.Command{
		Use:   "add NAME --type TYPE",
		Short: "Add a notification channel",
		Long: "add stores a new channel. The type is webhook (--url, optional --credential), smtp (--host, --from,\n" +
			"--to, optional --port, --security starttls or tls, and --credential for the login), telegram\n" +
			"(--chat-id and --credential for the bot token) or slack (--credential for the hook URL). The\n" +
			"credential is the name of a vault credential, never a secret. The result is validated first; a\n" +
			"channel with problems is not stored. The channels are pending approval until a person approves\n" +
			"them in the terminal interface. Nothing is sent by adding a channel.",
		Example: "  sysmin notify channel add ops-mail --type smtp --host smtp.example.com --from sysmin@example.com \\\n" +
			"      --to ops@example.com --security starttls --credential demo-smtp",
		Args: oneNamed("add", "channel"),
		RunE: func(c *cobra.Command, args []string) error {
			p.Name = args[0]
			var res control.ChangeResult
			err := withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.AddChannel(p)
				return err
			})
			if err != nil {
				return finishWith(c, opts, "notify channel add", true, err, channelError)
			}
			return channelResult(c, opts, p.Name, "added", res)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&p.Type, "type", "", "channel type: webhook, smtp, telegram or slack")
	fl.StringVar(&p.URL, "url", "", "webhook: the https URL; it must not contain a secret")
	fl.StringVar(&p.Host, "host", "", "smtp: the mail server")
	fl.IntVar(&p.Port, "port", 0, "smtp: the port (default by security)")
	fl.StringVar(&p.From, "from", "", "smtp: the sender address")
	fl.StringArrayVar(&p.To, "to", nil, "smtp: a recipient address; repeat for several")
	fl.StringVar(&p.Security, "security", "", "smtp: starttls or tls")
	fl.StringVar(&p.ChatID, "chat-id", "", "telegram: the chat to write to")
	fl.StringVar(&p.Credential, "credential", "", "name of the vault credential the channel needs")
	fl.StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newChannelListCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the stored channels",
		Long: "list shows the stored channels with all their settings. A credential is shown by name only. The\n" +
			"state is approved when the channel is part of the approved state exactly as stored, pending\n" +
			"approval when it is not, and unknown while the vault is locked.",
		Args: noArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var list []control.ChannelSummary
			err := withService(socket, func(cl *client.Client) (err error) {
				list, err = cl.Channels()
				return err
			})
			if err != nil {
				return channelError("notify channel list", err)
			}
			result := output.Collection{Columns: []string{"name", "type", "url", "host", "port", "from", "to", "security", "chat_id", "credential", "state"}}
			for _, ch := range list {
				result.Rows = append(result.Rows, output.Row{
					"name": ch.Name, "type": ch.Type, "url": ch.URL, "host": ch.Host, "port": int64(ch.Port), "from": ch.From,
					"to": strings.Join(ch.To, ","), "security": ch.Security, "chat_id": ch.ChatID, "credential": ch.Credential, "state": ch.State,
				})
			}
			return emit(c, opts, result)
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newChannelRemoveCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "remove NAME",
		Short: "Remove a notification channel",
		Long: "remove deletes the channel. Until a person approves the removal in the terminal interface, the\n" +
			"approved channels stay in effect.",
		Args: oneNamed("remove", "channel"),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.ChangeResult
			err := withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.RemoveChannel(args[0])
				return err
			})
			if err != nil {
				return finishWith(c, opts, "notify channel remove", true, err, channelError)
			}
			return channelResult(c, opts, args[0], "removed", res)
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newNotifyTestCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "test CHANNEL",
		Short: "Send a test message to an approved channel",
		Long: "test makes the service send one test message (event notify.test) to the channel and reports whether\n" +
			"the channel took it. Only approved channels can be tested; a channel that is stored but not yet\n" +
			"approved is reported as missing. The delivery is tried once and recorded in the audit log. The\n" +
			"vault must be unlocked.",
		Example: "  sysmin notify test ops-hook",
		Args:    oneNamed("test", "channel"),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.NotifyTestResult
			err := withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.TestChannel(args[0])
				return err
			})
			if err != nil {
				return testChannelError("notify test", err)
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "channel", Value: res.Channel},
				{Name: "event", Value: res.Event},
				{Name: "result", Value: "sent"},
			}})
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func testChannelError(what string, err error) error {
	var ce *control.Error
	if errors.As(err, &ce) {
		switch ce.Code {
		case control.CodeNotFound:
			return fmt.Errorf("%s: no approved channel has this name; check 'sysmin notify channel list'", what)
		case control.CodeSendFailed:
			return fmt.Errorf("%s: the channel did not take the message; see the audit log of the service", what)
		}
	}
	return channelError(what, err)
}
