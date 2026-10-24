package cmd

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rayli09/ibkr-cli/internal/apierr"
	"github.com/rayli09/ibkr-cli/internal/cpapi"
)

// The scanner group mirrors IBind's scanner_mixin (both the /iserver
// and the /hmds scanner surfaces).

func newScannerCmd(opts *options) *cobra.Command {
	c := &cobra.Command{
		Use:   "scanner",
		Short: "Market scanners (IBind scanner_mixin parity)",
	}
	c.AddCommand(newScannerParamsCmd(opts))
	c.AddCommand(newScannerRunCmd(opts))
	c.AddCommand(newScannerHMDSParamsCmd(opts))
	c.AddCommand(newScannerHMDSRunCmd(opts))
	return c
}

// parseScannerFilters converts repeated --filter code=value flags to
// IBind's [{"code":..., "value":...}] list; numeric values are sent
// as numbers, everything else as strings.
func parseScannerFilters(raw []string) ([]cpapi.ScannerFilter, error) {
	out := make([]cpapi.ScannerFilter, 0, len(raw))
	for _, f := range raw {
		code, val, ok := strings.Cut(f, "=")
		if !ok || code == "" {
			return nil, &apierr.UsageError{Msg: "--filter must be code=value, got " + f}
		}
		var v any = val
		if n, err := strconv.Atoi(val); err == nil {
			v = n
		} else if fl, err := strconv.ParseFloat(val, 64); err == nil {
			v = fl
		}
		out = append(out, cpapi.ScannerFilter{Code: code, Value: v})
	}
	return out, nil
}

func newScannerParamsCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "params",
		Short: "Scanner vocabulary: instruments, scan types, locations, filters (GET /iserver/scanner/params)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.ScannerParams(ctx())
			})
		},
	}
}

func newScannerRunCmd(opts *options) *cobra.Command {
	var instrument, scanType, location string
	var filters []string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a market scan, max 50 contracts (POST /iserver/scanner/run)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if instrument == "" || scanType == "" || location == "" {
				return &apierr.UsageError{Msg: "--instrument, --type and --location are required (see scanner params)"}
			}
			fs, err := parseScannerFilters(filters)
			if err != nil {
				return err
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.ScannerRun(ctx(), instrument, scanType, location, fs)
			})
		},
	}
	cmd.Flags().StringVar(&instrument, "instrument", "", "instrument type from scanner params (required)")
	cmd.Flags().StringVar(&scanType, "type", "", "scan type from scanner params (required)")
	cmd.Flags().StringVar(&location, "location", "", "location from scanner params (required)")
	cmd.Flags().StringArrayVar(&filters, "filter", nil, "filter as code=value (repeatable)")
	return cmd
}

func newScannerHMDSParamsCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "hmds-params",
		Short: "HMDS scanner vocabulary (GET /hmds/scanner/params)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.HMDSScannerParams(ctx())
			})
		},
	}
}

func newScannerHMDSRunCmd(opts *options) *cobra.Command {
	var instrument, location, scanCode, secType string
	var filters []string
	var maxItems int
	cmd := &cobra.Command{
		Use:   "hmds-run",
		Short: "Run an HMDS scan (POST /hmds/scanner/run)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if instrument == "" || location == "" || scanCode == "" || secType == "" {
				return &apierr.UsageError{Msg: "--instrument, --location, --scan-code and --sectype are required (see scanner hmds-params)"}
			}
			fs, err := parseScannerFilters(filters)
			if err != nil {
				return err
			}
			return opts.readCall(func(cl *cpapi.Client) (*cpapi.Response, error) {
				return cl.HMDSScannerRun(ctx(), instrument, location, scanCode, secType, fs, maxItems)
			})
		},
	}
	cmd.Flags().StringVar(&instrument, "instrument", "", "instrument (required)")
	cmd.Flags().StringVar(&location, "location", "", "location (required)")
	cmd.Flags().StringVar(&scanCode, "scan-code", "", "scan code (required)")
	cmd.Flags().StringVar(&secType, "sectype", "", "security type (required)")
	cmd.Flags().StringArrayVar(&filters, "filter", nil, "filter as code=value (repeatable)")
	cmd.Flags().IntVar(&maxItems, "max-items", 0, "max results")
	return cmd
}
