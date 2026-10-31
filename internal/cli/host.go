package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

// hostEnv is the caller and the databases the host commands work on.
type hostEnv struct {
	// UID is the effective user of the caller; root is UID 0.
	UID  int
	Host *hostadmin.Host
}

func (o *Options) hostEnv() *hostEnv {
	if o.host != nil {
		return o.host
	}
	uid := os.Geteuid()
	return &hostEnv{UID: uid, Host: hostadmin.NewHost(uid == 0)}
}

// requireAdmin lets root and members of the sudo group through. A failure to read the group database is
// treated as no access.
func (e *hostEnv) requireAdmin() error {
	if e.UID == 0 {
		return nil
	}
	if admin, err := e.Host.IsAdmin(e.UID); err == nil && admin {
		return nil
	}
	return &plainError{client.ReservedMessage}
}

func newHostCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "host",
		Short: "Show the users, groups and folder permissions of this host",
		Long: "The commands users, user, groups, group and folder read the local users, groups and folder permissions\n" +
			"and change nothing. Only root and members of the group sudo may use them. Whether an account is\n" +
			"locked or expired and which SSH keys it has are shown only to root; otherwise they are unknown.\n" +
			"The commands apply, undo and journal change the host and need root; the TUI runs them via sudo.\n" +
			"See 'sysmin help host-admin'.",
		Args: gated(opts, noVaultArgs),
		PersistentPreRunE: func(c *cobra.Command, args []string) error {
			if err := opts.hostEnv().requireAdmin(); err != nil {
				return err
			}
			return c.Root().PersistentPreRunE(c, args)
		},
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newHostUsersCommand(opts), newHostUserCommand(opts), newHostGroupsCommand(opts),
		newHostGroupCommand(opts), newHostFolderCommand(opts), newHostApplyCommand(opts), newHostUndoCommand(opts),
		newHostJournalCommand(opts))
	return cmd
}

// gated checks the caller before the arguments, so a caller without access learns nothing else.
func gated(opts *Options, check cobra.PositionalArgs) cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		if err := opts.hostEnv().requireAdmin(); err != nil {
			return err
		}
		return check(c, args)
	}
}

func exactlyOne(opts *Options, what string) cobra.PositionalArgs {
	return gated(opts, func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return newSyntaxError(fmt.Errorf("exactly one %s is needed, got %d arguments", what, len(args)))
		}
		return nil
	})
}

func hostError(what string, err error) error { return fmt.Errorf("%s: %w", what, err) }

func newHostUsersCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "users",
		Short: "List the local users",
		Args:  gated(opts, noVaultArgs),
		RunE: func(c *cobra.Command, _ []string) error {
			users, err := opts.hostEnv().Host.Users()
			if err != nil {
				return hostError("host users", err)
			}
			result := output.Collection{Columns: []string{"name", "uid", "primary_group", "groups", "home", "system",
				"admin", "docker", "enabled", "locked", "expired"}}
			for _, u := range users {
				result.Rows = append(result.Rows, output.Row{
					"name": u.Name, "uid": int64(u.UID), "primary_group": u.PrimaryGroup, "groups": u.Groups, "home": u.Home,
					"system": u.System, "admin": u.Admin, "docker": u.Docker, "enabled": u.Enabled,
					"locked": triState(opts, u.Locked), "expired": triState(opts, u.Expired),
				})
			}
			return emit(c, opts, result)
		},
	}
}

func newHostUserCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "user <name>",
		Short: "Show one local user",
		Args:  exactlyOne(opts, "user name"),
		RunE: func(c *cobra.Command, args []string) error {
			u, err := opts.hostEnv().Host.User(args[0])
			if err != nil {
				return hostError("host user", err)
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "name", Value: u.Name},
				{Name: "uid", Value: int64(u.UID)},
				{Name: "primary_group", Value: u.PrimaryGroup},
				{Name: "groups", Value: u.Groups},
				{Name: "home", Value: u.Home},
				{Name: "system", Value: u.System},
				{Name: "admin", Value: u.Admin},
				{Name: "docker", Value: u.Docker},
				{Name: "enabled", Value: u.Enabled},
				{Name: "locked", Value: triState(opts, u.Locked)},
				{Name: "expired", Value: triState(opts, u.Expired)},
				{Name: "ssh_keys", Value: keysValue(opts, u.SSHKeys)},
			}})
		},
	}
}

func newHostGroupsCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "groups",
		Short: "List the local groups",
		Args:  gated(opts, noVaultArgs),
		RunE: func(c *cobra.Command, _ []string) error {
			groups, err := opts.hostEnv().Host.Groups()
			if err != nil {
				return hostError("host groups", err)
			}
			result := output.Collection{Columns: []string{"name", "gid", "members", "system"}}
			for _, g := range groups {
				result.Rows = append(result.Rows, output.Row{
					"name": g.Name, "gid": int64(g.GID), "members": g.Members, "system": g.System,
				})
			}
			return emit(c, opts, result)
		},
	}
}

func newHostGroupCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "group <name>",
		Short: "Show one local group",
		Args:  exactlyOne(opts, "group name"),
		RunE: func(c *cobra.Command, args []string) error {
			g, err := opts.hostEnv().Host.Group(args[0])
			if err != nil {
				return hostError("host group", err)
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "name", Value: g.Name},
				{Name: "gid", Value: int64(g.GID)},
				{Name: "members", Value: g.Members},
				{Name: "system", Value: g.System},
			}})
		},
	}
}

func newHostFolderCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "folder <path>",
		Short: "Show owner, group and permissions of a folder or file",
		Args:  exactlyOne(opts, "path"),
		RunE: func(c *cobra.Command, args []string) error {
			f, err := opts.hostEnv().Host.Folder(args[0])
			if err != nil {
				return hostError("host folder", err)
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "path", Value: f.Path},
				{Name: "resolved", Value: f.Resolved},
				{Name: "kind", Value: f.Kind},
				{Name: "owner", Value: f.Owner},
				{Name: "group", Value: f.Group},
				{Name: "mode", Value: f.Mode},
				{Name: "owner_access", Value: string(f.OwnerAcc)},
				{Name: "group_access", Value: string(f.GroupAcc)},
				{Name: "others_access", Value: string(f.OthersAcc)},
				{Name: "acl", Value: triState(opts, f.ACL)},
				{Name: "setuid", Value: f.Setuid},
				{Name: "setgid", Value: f.Setgid},
				{Name: "sticky", Value: f.Sticky},
				{Name: "protected", Value: f.Protected},
			}})
		},
	}
}

// triState renders a value that may be unknown: null in JSON, "unknown" in the other formats.
func triState(opts *Options, v *bool) any {
	switch {
	case v != nil:
		return *v
	case opts.Format == output.FormatJSON:
		return nil
	}
	return "unknown"
}

// keysValue renders the keys of a user: a list of objects in JSON, one text elsewhere. Unknown is null in
// JSON and "unknown" elsewhere.
func keysValue(opts *Options, keys []hostadmin.SSHKey) any {
	if opts.Format == output.FormatJSON {
		if keys == nil {
			return nil
		}
		return keys
	}
	if keys == nil {
		return "unknown"
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = strings.TrimSpace(k.Fingerprint + " " + k.Type + " " + k.Comment)
	}
	return strings.Join(parts, "; ")
}
