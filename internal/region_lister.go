package internal

import (
	"context"
	"sort"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

var azureProviderRegions = []string{
	"eastus",
	"southeastasia",
	"westeurope",
	"westus2",
}

func (s *azureIaCServer) ListRegions(context.Context, *pb.ListRegionsRequest) (*pb.ListRegionsResponse, error) {
	regions := make([]string, len(azureProviderRegions))
	copy(regions, azureProviderRegions)
	sort.Strings(regions)

	out := make([]*pb.ProviderRegion, 0, len(regions))
	for _, name := range regions {
		out = append(out, &pb.ProviderRegion{Name: name, DisplayName: name})
	}
	return &pb.ListRegionsResponse{Regions: out}, nil
}
