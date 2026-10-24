package cmd

import (
	"encoding/json"
	"time"

	"github.com/spf13/cobra"

	"github.com/rayli09/ibkr-cli/internal/apierr"
	"github.com/rayli09/ibkr-cli/internal/cpapi"
)

// The fa group mirrors IBind's fa_mixin (financial-advisor model
// portfolios + presets). Mutations (model-save, invest-divest,
// submit-transfers, preset-save) use the standard dry-run rail.

// reqIDOrNow returns the --req-id flag value, or a time-derived id
// when the flag is 0 (IBind takes req_id from its caller; the CLI
// derives one so ad-hoc calls stay unique).
func reqIDOrNow(flagVal int) int {
	if flagVal != 0 {
		return flagVal
	}
	return int(time.Now().Unix())
}

func newFACmd(opts *options) *cobra.Command {
	c := &cobra.Command{
		Use:   "fa",
		Short: "Financial-advisor models and presets (IBind fa_mixin parity)",
	}
	c.AddCommand(newFAModelsCmd(opts))
	c.AddCommand(newFAModelPositionsCmd(opts))
	c.AddCommand(newFAModelSummaryCmd(opts))
	c.AddCommand(newFAModelAccountsCmd(opts))
	c.AddCommand(newFAModelSaveCmd(opts))
	c.AddCommand(newFAInvestDivestCmd(opts))
	c.AddCommand(newFAInvestedCmd(opts))
	c.AddCommand(newFASubmitTransfersCmd(opts))
	c.AddCommand(newFAPresetCmd(opts))
	c.AddCommand(newFAPresetSaveCmd(opts))
	return c
}

func newFAModelsCmd(opts *options) *cobra.Command {
	var reqID int
	cmd := &cobra.Command{
		Use:   "models",
		Short: "List model portfolios (POST /fa/model/list)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.FAModelList(ctx(), reqIDOrNow(reqID))
			})
		},
	}
	cmd.Flags().IntVar(&reqID, "req-id", 0, "request id (default: derived from the current time)")
	return cmd
}

func newFAModelPositionsCmd(opts *options) *cobra.Command {
	var reqID, limit int
	var model, sortField, sortDirection string
	cmd := &cobra.Command{
		Use:   "model-positions",
		Short: "One model's target positions (POST /fa/model/positions; values are fractions in [0,1])",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.FAModelPositions(ctx(), reqIDOrNow(reqID), model, sortField, sortDirection, limit)
			})
		},
	}
	addFAReqModelFlags(cmd, &reqID, &model)
	cmd.Flags().StringVar(&sortField, "sort-field", "", "sort field")
	cmd.Flags().StringVar(&sortDirection, "sort-direction", "", "sort direction")
	cmd.Flags().IntVar(&limit, "limit", 0, "row limit")
	return cmd
}

func newFAModelSummaryCmd(opts *options) *cobra.Command {
	var reqID int
	var model string
	cmd := &cobra.Command{
		Use:   "model-summary",
		Short: "One model's summary (POST /fa/model/summary)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.FAModelSummary(ctx(), reqIDOrNow(reqID), model)
			})
		},
	}
	addFAReqModelFlags(cmd, &reqID, &model)
	return cmd
}

func newFAModelAccountsCmd(opts *options) *cobra.Command {
	var reqID int
	var model string
	var calcPnLs bool
	cmd := &cobra.Command{
		Use:   "model-accounts",
		Short: "Accounts invested in a model (POST /fa/model/accounts-details)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.FAModelAccounts(ctx(), reqIDOrNow(reqID), model, calcPnLs)
			})
		},
	}
	addFAReqModelFlags(cmd, &reqID, &model)
	cmd.Flags().BoolVar(&calcPnLs, "calc-pnls", false, "include calculated PnLs")
	return cmd
}

func newFAModelSaveCmd(opts *options) *cobra.Command {
	var reqID int
	var model, desc, cashTargets, positionTargets string
	var isStatic, confirm bool
	cmd := &cobra.Command{
		Use:   "model-save",
		Short: "Create/modify a model's targets (POST /fa/model/save); requires --confirm",
		Long: `Targets are JSON arrays: --cash-targets '[{"ccy":"USD","target":0.1}]',
--position-targets '[{"conid":265598,"target":0.9}]'. Values are
fractions in [0, 1] and must sum to 1.0 across both lists (IBind's
live observation; 0-100 percentages are rejected upstream).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if model == "" || desc == "" {
				return &apierr.UsageError{Msg: "--model and --desc are required (omitting desc is a bare 400 upstream)"}
			}
			cash, err := jsonRawOrEmpty(cashTargets)
			if err != nil {
				return err
			}
			pos, err := jsonRawOrEmpty(positionTargets)
			if err != nil {
				return err
			}
			payload := map[string]any{
				"reqID": reqIDOrNow(reqID), "model": model, "desc": desc, "isStatic": isStatic,
			}
			if len(cash) > 0 {
				payload["cashTargets"] = cash
			}
			if len(pos) > 0 {
				payload["positionTargets"] = pos
			}
			return opts.mutateCall("fa model-save", "", confirm, payload,
				func(cl *cpapi.Client) (*cpapi.Response, error) {
					return cl.FAModelSave(ctx(), reqIDOrNow(reqID), model, desc, isStatic, cash, pos)
				})
		},
	}
	addFAReqModelFlags(cmd, &reqID, &model)
	cmd.Flags().StringVar(&desc, "desc", "", "model description (required)")
	cmd.Flags().BoolVar(&isStatic, "static", false, "static model (targets never drift-adjust)")
	cmd.Flags().StringVar(&cashTargets, "cash-targets", "", "cash targets JSON array")
	cmd.Flags().StringVar(&positionTargets, "position-targets", "", "position targets JSON array")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually save (without it: dry run)")
	return cmd
}

func newFAInvestDivestCmd(opts *options) *cobra.Command {
	var reqID int
	var model, accountList string
	var confirm bool
	cmd := &cobra.Command{
		Use:   "invest-divest",
		Short: "Draft invest/divest transfers into a model (POST /fa/model/invest-divest); requires --confirm",
		Long: `--account-list is a JSON array like
'[{"account":"DU123","amtToInvest":10000}]'; an amtToInvest of 0
divests the account fully. The response's transfersInstructionId
is the fpOrderId for fa submit-transfers.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := jsonRawOrEmpty(accountList)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				return &apierr.UsageError{Msg: "--account-list is required"}
			}
			payload := map[string]any{
				"reqID": reqIDOrNow(reqID), "model": model, "accountList": list,
			}
			return opts.mutateCall("fa invest-divest", "", confirm, payload,
				func(cl *cpapi.Client) (*cpapi.Response, error) {
					return cl.FAInvestDivest(ctx(), reqIDOrNow(reqID), model, list)
				})
		},
	}
	addFAReqModelFlags(cmd, &reqID, &model)
	cmd.Flags().StringVar(&accountList, "account-list", "", "account list JSON array (required)")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually draft the transfers (without it: dry run)")
	return cmd
}

func newFAInvestedCmd(opts *options) *cobra.Command {
	var reqID int
	var model, subscriptionStatus string
	cmd := &cobra.Command{
		Use:   "invested",
		Short: "Positions from invest/divest (POST /fa/model/invest-divest-positions)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.FAInvestedPositions(ctx(), reqIDOrNow(reqID), model, subscriptionStatus)
			})
		},
	}
	addFAReqModelFlags(cmd, &reqID, &model)
	cmd.Flags().StringVar(&subscriptionStatus, "subscription-status", "", "subscription status filter")
	return cmd
}

func newFASubmitTransfersCmd(opts *options) *cobra.Command {
	var reqID int
	var fpOrderID int64
	var confirm bool
	cmd := &cobra.Command{
		Use:   "submit-transfers",
		Short: "Execute a drafted transfer instruction (POST /fa/model/submit-transfers); requires --confirm",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if fpOrderID == 0 {
				return &apierr.UsageError{Msg: "--fp-order-id is required (the transfersInstructionId from fa invest-divest)"}
			}
			payload := map[string]any{"reqID": reqIDOrNow(reqID), "fpOrderId": fpOrderID}
			return opts.mutateCall("fa submit-transfers", "", confirm, payload,
				func(cl *cpapi.Client) (*cpapi.Response, error) {
					return cl.FASubmitTransfers(ctx(), reqIDOrNow(reqID), fpOrderID)
				})
		},
	}
	cmd.Flags().IntVar(&reqID, "req-id", 0, "request id (default: derived from the current time)")
	cmd.Flags().Int64Var(&fpOrderID, "fp-order-id", 0, "draft instruction id from fa invest-divest (required)")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually submit (without it: dry run)")
	return cmd
}

func newFAPresetCmd(opts *options) *cobra.Command {
	var reqID int
	cmd := &cobra.Command{
		Use:   "preset",
		Short: "Read the FA preset flags (POST /fa/fa-preset/get)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.FAPresetGet(ctx(), reqIDOrNow(reqID))
			})
		},
	}
	cmd.Flags().IntVar(&reqID, "req-id", 0, "request id (default: derived from the current time)")
	return cmd
}

// faPresetFlags maps CLI flag names to IBind's fa_preset_save keys.
var faPresetFlags = []struct{ flag, key string }{
	{"avoid-negative-cash", "avoidNegativeCashInIndependent"},
	{"close-divest-independent", "closeDivestIndependentPosition"},
	{"fully-invest-long", "fullyInvestExistingLongPositions"},
	{"keep-model-open", "keepModelOpen"},
	{"prefer-cross-independent", "preferCrossWithIndependent"},
	{"prefer-transfer-independent", "preferTransferFromIndependent"},
	{"round-to-board-lot", "roundAllocationQuantityToExchangeBoardLot"},
	{"use-non-base-ccy", "useNonBaseCcy"},
	{"use-tolerance-range", "useToleranceRange"},
}

func newFAPresetSaveCmd(opts *options) *cobra.Command {
	var reqID int
	var confirm bool
	vals := make(map[string]*bool, len(faPresetFlags))
	cmd := &cobra.Command{
		Use:   "preset-save",
		Short: "Save FA preset flags (POST /fa/fa-preset/save); requires --confirm",
		Long:  "Only the preset flags you pass are sent; unset flags are left unchanged upstream.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := map[string]bool{}
			for _, pf := range faPresetFlags {
				if cmd.Flags().Changed(pf.flag) {
					flags[pf.key] = *vals[pf.flag]
				}
			}
			if len(flags) == 0 {
				return &apierr.UsageError{Msg: "pass at least one preset flag (see --help)"}
			}
			payload := map[string]any{"reqID": reqIDOrNow(reqID)}
			for k, v := range flags {
				payload[k] = v
			}
			return opts.mutateCall("fa preset-save", "", confirm, payload,
				func(cl *cpapi.Client) (*cpapi.Response, error) {
					return cl.FAPresetSave(ctx(), reqIDOrNow(reqID), flags)
				})
		},
	}
	cmd.Flags().IntVar(&reqID, "req-id", 0, "request id (default: derived from the current time)")
	for _, pf := range faPresetFlags {
		b := new(bool)
		vals[pf.flag] = b
		cmd.Flags().BoolVar(b, pf.flag, false, "preset: "+pf.key)
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually save (without it: dry run)")
	return cmd
}

func addFAReqModelFlags(cmd *cobra.Command, reqID *int, model *string) {
	cmd.Flags().IntVar(reqID, "req-id", 0, "request id (default: derived from the current time)")
	cmd.Flags().StringVar(model, "model", "", "model name (required)")
	_ = cmd.MarkFlagRequired("model")
}

func jsonRawOrEmpty(s string) (json.RawMessage, error) {
	if s == "" {
		return nil, nil
	}
	if !json.Valid([]byte(s)) {
		return nil, &apierr.UsageError{Msg: "invalid JSON: " + s}
	}
	return json.RawMessage(s), nil
}
