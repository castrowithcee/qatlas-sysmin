package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

// maxTextBody shortens a release text in the table and compact formats, where a row is one line; JSON
// carries it whole.
const maxTextBody = 400

func newProjectChangelogCommand(opts *Options) *cobra.Command {
	var socket, image, to string
	cmd := &cobra.Command{
		Use:   "changelog PROJECT",
		Short: "Show the release notes between the current and the target version of a project",
		Long: "changelog reads the GitHub releases of the repository behind each image or git source of the approved\n" +
			"state of the project and shows those above the current version up to the target version, newest\n" +
			"first. The repository is the changelog_repo of the version rule, else the image label\n" +
			"org.opencontainers.image.source or, for a git source, its address. Without matching releases or\n" +
			"for another host the command shows a link to the repository. The target is the one the version\n" +
			"rule selects unless --to names a version. A changelog_credential of kind secret in the version rule\n" +
			"supplies a GitHub token. When GitHub is not reachable or limits requests, an earlier answer from\n" +
			"the cache is shown with a notice. Nothing is changed, and no admin session is needed, but the vault\n" +
			"must be unlocked. The command exits with an error when a source cannot be read; each source is\n" +
			"tried regardless of the others.",
		Example: "  sysmin project changelog demo-app --image app --format json\n" +
			"  sysmin project changelog demo-app --to 1.4.0",
		Args: oneName("changelog"),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.ChangelogResult
			err := withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.Changelog(args[0], image, to)
				return err
			})
			if err != nil {
				return changelogError(err)
			}
			result := output.Collection{Columns: []string{"source", "kind", "ref", "status", "current", "target", "repo", "version", "name", "published", "url", "body", "link", "notice", "problems", "message"}}
			failed := 0
			for _, s := range res.Sources {
				status := "ok"
				switch {
				case s.Error != "":
					status = s.Error
					failed++
				case s.Stale:
					status = "stale"
				}
				var problems []string
				for _, p := range s.Problems {
					problems = append(problems, p.Code+": "+p.Message)
				}
				row := output.Row{
					"source": s.Name, "kind": s.Kind, "ref": s.Ref, "status": status, "current": s.Current, "target": s.Target,
					"repo": s.Repo, "link": s.Link, "notice": s.Notice, "problems": strings.Join(problems, "; "), "message": s.Message,
				}
				if len(s.Releases) == 0 {
					result.Rows = append(result.Rows, row)
					continue
				}
				for _, r := range s.Releases {
					rr := output.Row{}
					for k, v := range row {
						rr[k] = v
					}
					body := r.Body
					if opts.Format != output.FormatJSON {
						body = shorten(body, maxTextBody)
					}
					rr["version"], rr["name"], rr["url"], rr["body"] = r.Version, r.Name, r.URL, body
					if !r.Published.IsZero() {
						rr["published"] = r.Published.UTC().Format(time.DateOnly)
					}
					result.Rows = append(result.Rows, rr)
				}
			}
			if err := emit(c, opts, result); err != nil {
				return err
			}
			if failed > 0 {
				return fmt.Errorf("project changelog: %d of %d sources could not be read", failed, len(res.Sources))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&image, "image", "", "show only this image of the project")
	cmd.Flags().StringVar(&to, "to", "", "show the releases up to this version instead of the selected target")
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

// changelogError turns the codes of a changelog request into messages that say what to do.
func changelogError(err error) error {
	if ce := new(control.Error); errors.As(err, &ce) && ce.Code == control.CodeBadRequest {
		return fmt.Errorf("project changelog: the service did not accept the request; check the project, --image and --to")
	}
	return sourceError("project changelog", err)
}

func shorten(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "..."
}
