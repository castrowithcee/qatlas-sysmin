package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

const logDateLayout = "2006-01-02"

func newLogCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Work with the tamper-evident action log",
		Args:  noLogArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newLogVerifyCommand(opts))
	return cmd
}

func newLogVerifyCommand(opts *Options) *cobra.Command {
	var dir, from, to string
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Check the hash chain of the action log, day by day",
		Long: "verify walks every day file of the action log and checks that its entries chain together without a\n" +
			"change, a swap, or a deletion that no retention cut documents. An unreadable line, such as one torn\n" +
			"by a crash, that a marker entry right after it documents leaves the chain intact and is counted as\n" +
			"damaged. This command takes no key, so it reports signatures as not checked.",
		Args: noLogArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			fromDay, toDay, err := parseLogRange(from, to)
			if err != nil {
				return newSyntaxError(err)
			}
			report, err := auditlog.Verify(dir, nil)
			if err != nil {
				return fmt.Errorf("verify the action log: %w", err)
			}
			result := output.Collection{Columns: []string{"day", "entries", "chain", "damaged", "signatures", "detail"}}
			broken := false
			for _, d := range report.Days {
				if (fromDay != "" && d.Date < fromDay) || (toDay != "" && d.Date > toDay) {
					continue
				}
				chain, detail := "intact", ""
				if d.Problem != "" {
					chain, detail = "broken", d.Problem
					broken = true
				}
				result.Rows = append(result.Rows, output.Row{
					"day": d.Date, "entries": int64(d.Entries), "chain": chain, "damaged": int64(d.Damaged),
					"signatures": d.SignatureState(report.Checked), "detail": detail,
				})
			}
			if err := emit(c, opts, result); err != nil {
				return err
			}
			if broken {
				return errors.New("the action log chain is broken")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", auditlog.DefaultDir, "directory holding the log files")
	cmd.Flags().StringVar(&from, "from", "", "first day to report, as YYYY-MM-DD")
	cmd.Flags().StringVar(&to, "to", "", "last day to report, as YYYY-MM-DD")
	return cmd
}

func noLogArgs(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return newSyntaxError(fmt.Errorf("unexpected argument %q", args[0]))
	}
	return nil
}

// parseLogRange validates the --from and --to days; an empty value leaves that end open.
func parseLogRange(from, to string) (string, string, error) {
	for _, d := range []struct{ flag, value string }{{"--from", from}, {"--to", to}} {
		if d.value == "" {
			continue
		}
		if _, err := time.Parse(logDateLayout, d.value); err != nil {
			return "", "", fmt.Errorf("%s must be a date as YYYY-MM-DD", d.flag)
		}
	}
	if from != "" && to != "" && from > to {
		return "", "", errors.New("--from must not be after --to")
	}
	return from, to, nil
}
