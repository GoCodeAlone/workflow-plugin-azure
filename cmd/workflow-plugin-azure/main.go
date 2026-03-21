// Command workflow-plugin-azure is a workflow engine external plugin that
// provides Microsoft Azure infrastructure provisioning via the IaC provider
// interface. It runs as a subprocess and communicates with the host workflow
// engine via the go-plugin protocol.
package main

import (
	"github.com/GoCodeAlone/workflow-plugin-azure/internal"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

var version = "dev"

func main() {
	sdk.Serve(internal.New(version))
}
