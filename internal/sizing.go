package internal

import (
	"github.com/GoCodeAlone/workflow/interfaces"
)

// azureSizing maps abstract size tiers to Azure VM instance types.
var azureSizing = map[interfaces.Size]string{
	interfaces.SizeXS: "Standard_B1s",
	interfaces.SizeS:  "Standard_B2s",
	interfaces.SizeM:  "Standard_D2s_v5",
	interfaces.SizeL:  "Standard_D4s_v5",
	interfaces.SizeXL: "Standard_D8s_v5",
}

// dbSizing maps abstract size tiers to Azure SQL DTU tiers.
var dbSizing = map[interfaces.Size]dbTier{
	interfaces.SizeXS: {edition: "Basic", serviceTier: "B", dtu: 5, vCores: 0},
	interfaces.SizeS:  {edition: "Standard", serviceTier: "S1", dtu: 20, vCores: 0},
	interfaces.SizeM:  {edition: "Standard", serviceTier: "S3", dtu: 100, vCores: 0},
	interfaces.SizeL:  {edition: "Premium", serviceTier: "P1", dtu: 125, vCores: 4},
	interfaces.SizeXL: {edition: "Premium", serviceTier: "P2", dtu: 250, vCores: 8},
}

type dbTier struct {
	edition     string
	serviceTier string
	dtu         int
	vCores      int
}

// cacheSizing maps abstract size tiers to Azure Cache for Redis SKUs.
var cacheSizing = map[interfaces.Size]string{
	interfaces.SizeXS: "C0",
	interfaces.SizeS:  "C1",
	interfaces.SizeM:  "C2",
	interfaces.SizeL:  "C3",
	interfaces.SizeXL: "C4",
}

// resolveInstanceType returns the Azure VM instance type for the given size.
func resolveInstanceType(size interfaces.Size) string {
	if t, ok := azureSizing[size]; ok {
		return t
	}
	return azureSizing[interfaces.SizeS]
}
