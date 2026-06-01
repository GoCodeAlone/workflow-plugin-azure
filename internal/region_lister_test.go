package internal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/grpc"
)

func TestAzureIaCServer_ListRegions(t *testing.T) {
	resp, err := NewIaCServer().ListRegions(context.Background(), &pb.ListRegionsRequest{EnvName: "prod"})
	if err != nil {
		t.Fatalf("ListRegions: %v", err)
	}
	got := regionNames(resp.GetRegions())
	want := append([]string(nil), azureFallbackRegions...)
	sort.Strings(want)
	if !sameStrings(got, want) {
		t.Fatalf("regions = %v, want %v", got, want)
	}
}

func TestAzureIaCServer_RegistersRegionLister(t *testing.T) {
	server := grpc.NewServer()
	if err := sdk.RegisterAllIaCProviderServices(server, NewIaCServer()); err != nil {
		t.Fatalf("RegisterAllIaCProviderServices: %v", err)
	}
	if _, ok := server.GetServiceInfo()[pb.IaCProviderRegionLister_ServiceDesc.ServiceName]; !ok {
		t.Fatalf("registered services missing %s", pb.IaCProviderRegionLister_ServiceDesc.ServiceName)
	}
}

func TestPluginManifestAdvertisesRegionLister(t *testing.T) {
	assertManifestAdvertisesRegionLister(t, filepath.Join(hostConformanceRepoRoot(t), "plugin.json"))
	assertManifestAdvertisesRegionLister(t, filepath.Join(hostConformanceRepoRoot(t), "cmd", "workflow-plugin-azure", "plugin.json"))
}

func assertManifestAdvertisesRegionLister(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var manifest struct {
		IaCServices []string `json:"iacServices"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if !containsString(manifest.IaCServices, pb.IaCProviderRegionLister_ServiceDesc.ServiceName) {
		t.Fatalf("%s iacServices missing %s: %v", path, pb.IaCProviderRegionLister_ServiceDesc.ServiceName, manifest.IaCServices)
	}
}

func regionNames(regions []*pb.ProviderRegion) []string {
	out := make([]string, 0, len(regions))
	for _, region := range regions {
		out = append(out, region.GetName())
		if region.GetDisplayName() == "" {
			out = append(out, "<empty-display>")
		}
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
