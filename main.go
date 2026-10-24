// Command ibkr-cli is a Go command-line client for the Interactive
// Brokers Client Portal Web API 1.0 (CPAPI 1.0), modeled on
// schwab-cli and etrade-cli.
//
// See README.md for the command list, the two authentication modes
// (local Client Portal Gateway via IBeam, or direct OAuth 1.0a), and
// the dry-run order safety rails. The design specification lives in
// docs/SPEC.md.
package main

import "github.com/rayli09/ibkr-cli/cmd"

func main() {
	cmd.Execute()
}
