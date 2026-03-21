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

// dbSizing maps abstract size tiers to Azure SQL Database vCore SKUs (General Purpose, Gen5).
var dbSizing = map[interfaces.Size]dbTier{
	interfaces.SizeXS: {skuName: "GP_Gen5_1", vCores: 1},
	interfaces.SizeS:  {skuName: "GP_Gen5_1", vCores: 1},
	interfaces.SizeM:  {skuName: "GP_Gen5_2", vCores: 2},
	interfaces.SizeL:  {skuName: "GP_Gen5_4", vCores: 4},
	interfaces.SizeXL: {skuName: "GP_Gen5_8", vCores: 8},
}

type dbTier struct {
	skuName string
	vCores  int
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
