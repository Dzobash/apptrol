// Package examples holds the example configuration, built into the binary so
// that Apptrol can create it on first start (CFG-09).
package examples

import _ "embed"

// Config is the contents of examples/config.toml.
//
//go:embed config.toml
var Config []byte
