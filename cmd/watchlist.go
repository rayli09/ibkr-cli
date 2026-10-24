package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rayli09/ibkr-cli/internal/cpapi"
)

// The watchlist group mirrors IBind's watchlist_mixin. Create and
// delete are mutations: dry-run by default, --confirm to send,
// audit-logged — the same rail as orders.

func newWatchlistCmd(opts *options) *cobra.Command {
	c := &cobra.Command{
		Use:   "watchlist",
		Short: "User watchlists (IBind watchlist_mixin parity)",
	}
	c.AddCommand(newWatchlistListCmd(opts))
	c.AddCommand(newWatchlistGetCmd(opts))
	c.AddCommand(newWatchlistCreateCmd(opts))
	c.AddCommand(newWatchlistDeleteCmd(opts))
	return c
}

func newWatchlistListCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "All user watchlists (GET /iserver/watchlists)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.AllWatchlists(ctx())
			})
		},
	}
}

func newWatchlistGetCmd(opts *options) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "get",
		Short: "One watchlist's contents (GET /iserver/watchlist)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.WatchlistInfo(ctx(), id)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "watchlist id (required)")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newWatchlistCreateCmd(opts *options) *cobra.Command {
	var id, name, conidsFlag string
	var confirm bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a watchlist (POST /iserver/watchlist); requires --confirm",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var conids []int64
			var err error
			if conidsFlag != "" {
				conids, err = parseConids(strings.Split(conidsFlag, ","))
				if err != nil {
					return err
				}
			}
			rows := make([]map[string]any, len(conids))
			for i, cid := range conids {
				rows[i] = map[string]any{"C": cid}
			}
			payload := map[string]any{"name": name, "rows": rows}
			if id != "" {
				payload["id"] = id
			}
			return opts.mutateCall("watchlist create", "", confirm, payload,
				func(cl *cpapi.Client) (*cpapi.Response, error) {
					return cl.CreateWatchlist(ctx(), id, name, conids)
				})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "watchlist name (required)")
	_ = cmd.MarkFlagRequired("name")
	cmd.Flags().StringVar(&id, "id", "", "watchlist id (assigned by the server when omitted)")
	cmd.Flags().StringVar(&conidsFlag, "conids", "", "comma-separated conids for the rows")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually create (without it: dry run)")
	return cmd
}

func newWatchlistDeleteCmd(opts *options) *cobra.Command {
	var id string
	var confirm bool
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a watchlist (DELETE /iserver/watchlist); shows it first, sends only with --confirm",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := opts.env()
			if err != nil {
				return err
			}
			info, err := env.client.WatchlistInfo(ctx(), id)
			if err != nil {
				return err
			}
			fmt.Printf("watchlist %s:\n", id)
			printBody(info.Body)
			if !confirm {
				fmt.Println("DRY RUN — nothing was sent. Re-run with --confirm to delete this watchlist.")
				return nil
			}
			if err := env.provider.EnsureSession(ctx()); err != nil {
				return err
			}
			resp, err := env.client.DeleteWatchlist(ctx(), id)
			if resp != nil {
				opts.auditCall("watchlist delete", "", []byte(fmt.Sprintf(`{"id":%q}`, id)), resp.Body, "")
			}
			if err != nil {
				return err
			}
			fmt.Println("deleted:")
			printBody(resp.Body)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "watchlist id (required)")
	_ = cmd.MarkFlagRequired("id")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually delete (without it: show the watchlist only)")
	return cmd
}
