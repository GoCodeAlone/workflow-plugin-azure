package internal

import (
	"context"
	"sort"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

var azureFallbackRegions = []string{
	"australiacentral",
	"australiacentral2",
	"australiaeast",
	"australiasoutheast",
	"brazilsouth",
	"brazilsoutheast",
	"canadacentral",
	"canadaeast",
	"centralindia",
	"centralus",
	"centraluseuap",
	"eastasia",
	"eastus",
	"eastus2",
	"eastus2euap",
	"francecentral",
	"francesouth",
	"germanynorth",
	"germanywestcentral",
	"israelcentral",
	"italynorth",
	"japaneast",
	"japanwest",
	"jioindiacentral",
	"jioindiawest",
	"koreacentral",
	"koreasouth",
	"malaysiasouth",
	"mexicocentral",
	"newzealandnorth",
	"northcentralus",
	"northeurope",
	"norwayeast",
	"norwaywest",
	"polandcentral",
	"qatarcentral",
	"southafricanorth",
	"southafricawest",
	"southcentralus",
	"southeastasia",
	"southindia",
	"spaincentral",
	"swedencentral",
	"switzerlandnorth",
	"switzerlandwest",
	"uaecentral",
	"uaenorth",
	"uksouth",
	"ukwest",
	"westcentralus",
	"westeurope",
	"westindia",
	"westus",
	"westus2",
	"westus3",
}

func (s *azureIaCServer) ListRegions(ctx context.Context, _ *pb.ListRegionsRequest) (*pb.ListRegionsResponse, error) {
	if s != nil && s.provider != nil {
		subscriptionID, cred, ok := s.provider.SubscriptionSnapshot()
		if ok {
			regions, err := listAzureRegions(ctx, subscriptionID, cred)
			if err == nil {
				return providerRegionsResponse(regions), nil
			}
		}
	}
	return providerRegionsResponse(regionEntries(azureFallbackRegions, nil)), nil
}

func listAzureRegions(ctx context.Context, subscriptionID string, cred azcore.TokenCredential) ([]*pb.ProviderRegion, error) {
	client, err := armsubscriptions.NewClient(cred, nil)
	if err != nil {
		return nil, err
	}
	pager := client.NewListLocationsPager(subscriptionID, nil)
	var regions []*pb.ProviderRegion
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, location := range page.Value {
			if location == nil || location.Name == nil || *location.Name == "" {
				continue
			}
			display := stringValue(location.DisplayName)
			if display == "" {
				display = *location.Name
			}
			regions = append(regions, &pb.ProviderRegion{Name: *location.Name, DisplayName: display})
		}
	}
	return regions, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func regionEntries(names []string, displayNames map[string]string) []*pb.ProviderRegion {
	out := make([]*pb.ProviderRegion, 0, len(names))
	for _, name := range names {
		display := displayNames[name]
		if display == "" {
			display = name
		}
		out = append(out, &pb.ProviderRegion{Name: name, DisplayName: display})
	}
	return out
}

func providerRegionsResponse(regions []*pb.ProviderRegion) *pb.ListRegionsResponse {
	regions = append([]*pb.ProviderRegion(nil), regions...)
	sort.Slice(regions, func(i, j int) bool {
		return regions[i].GetName() < regions[j].GetName()
	})
	return &pb.ListRegionsResponse{Regions: regions}
}
