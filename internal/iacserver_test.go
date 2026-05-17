// Package internal exercises the azureIaCServer typed gRPC methods.
// Tests use a real *AzureProvider with no initialized Azure session;
// only methods that do NOT require a live Azure credential are covered here.
// provider_test.go covers Version, Capabilities, and Plan via the *AzureProvider
// surface; Initialize (error path), drift detection, and compile guards live here.
package internal

import (
	"context"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

func TestNewIaCServer_NotNil(t *testing.T) {
	s := NewIaCServer()
	if s == nil {
		t.Fatal("NewIaCServer returned nil")
	}
}

func TestIaCServer_Name(t *testing.T) {
	s := NewIaCServer()
	resp, err := s.Name(context.Background(), &pb.NameRequest{})
	if err != nil {
		t.Fatalf("Name: %v", err)
	}
	if resp.GetName() != "azure" {
		t.Errorf("Name = %q, want %q", resp.GetName(), "azure")
	}
}

func TestIaCServer_Version(t *testing.T) {
	s := NewIaCServer()
	resp, err := s.Version(context.Background(), &pb.VersionRequest{})
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if resp.GetVersion() == "" {
		t.Error("Version returned empty string")
	}
}

func TestIaCServer_Capabilities(t *testing.T) {
	s := NewIaCServer()
	resp, err := s.Capabilities(context.Background(), &pb.CapabilitiesRequest{})
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	found := false
	for _, c := range resp.GetCapabilities() {
		if c.GetResourceType() == "infra.container_service" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Capabilities missing infra.container_service, got: %v", resp.GetCapabilities())
	}
}

func TestIaCServer_Initialize_EmptyConfig(t *testing.T) {
	s := NewIaCServer()
	// Empty config — AzureProvider.Initialize returns "azure: subscription_id is required".
	_, err := s.Initialize(context.Background(), &pb.InitializeRequest{ConfigJson: []byte(`{}`)})
	if err == nil {
		t.Error("expected error from Initialize with empty config (subscription_id required)")
	}
}

func TestIaCServer_CompileTimeGuards(t *testing.T) {
	// This test exists to document the compile-time guards.
	// If any of the interface assertions below fail to compile, this file will not build.
	var _ pb.IaCProviderRequiredServer = (*azureIaCServer)(nil)
	var _ pb.IaCProviderDriftDetectorServer = (*azureIaCServer)(nil)
	var _ pb.ResourceDriverServer = (*azureIaCServer)(nil)
}

func TestIaCServer_DetectDrift_Uninitialized(t *testing.T) {
	s := NewIaCServer()
	refs := []*pb.ResourceRef{{Name: "test", Type: "infra.container_service"}}
	resp, err := s.DetectDrift(context.Background(), &pb.DetectDriftRequest{Refs: refs})
	// Azure-specific behavior: AzureProvider.DetectDrift swallows driver lookup errors
	// (returns DriftResult{Drifted: false}, nil instead of an error). This differs from
	// the AWS pattern which returns an "not initialized" error.
	if err != nil {
		t.Fatalf("DetectDrift unexpected error: %v", err)
	}
	if len(resp.GetDrifts()) != 1 {
		t.Fatalf("expected 1 drift result, got %d", len(resp.GetDrifts()))
	}
	if resp.GetDrifts()[0].GetDrifted() {
		t.Error("expected Drifted=false for uninitialized provider")
	}
}

func TestIaCServer_DetectDriftWithSpecs_DelegatesToDetectDrift(t *testing.T) {
	s := NewIaCServer()
	refs := []*pb.ResourceRef{{Name: "test", Type: "infra.container_service"}}
	resp, err := s.DetectDriftWithSpecs(context.Background(), &pb.DetectDriftWithSpecsRequest{Refs: refs})
	// Same Azure-specific behavior: delegates to DetectDrift which swallows errors.
	if err != nil {
		t.Fatalf("DetectDriftWithSpecs unexpected error: %v", err)
	}
	if len(resp.GetDrifts()) != 1 {
		t.Fatalf("expected 1 drift result, got %d", len(resp.GetDrifts()))
	}
	if resp.GetDrifts()[0].GetDrifted() {
		t.Error("expected Drifted=false for uninitialized provider")
	}
}

// TestAzureIaCServer_Capabilities_ComputePlanVersionV2 pins the Phase 2
// contract signal: the plugin MUST declare ComputePlanVersion="v2" so
// wfctl routes via wfctlhelpers.ApplyPlanWithHooks (v2 dispatch).
// Per workflow#640 + #695 Phase 2 + 2.5 cascade closeout.
func TestAzureIaCServer_Capabilities_ComputePlanVersionV2(t *testing.T) {
	s := NewIaCServer()
	resp, err := s.Capabilities(context.Background(), &pb.CapabilitiesRequest{})
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if got := resp.GetComputePlanVersion(); got != "v2" {
		t.Errorf("CapabilitiesResponse.ComputePlanVersion = %q; want %q (v2 dispatch opt-in lost)", got, "v2")
	}
}
