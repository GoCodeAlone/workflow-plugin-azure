package internal

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestAzureProvider_Name(t *testing.T) {
	p := New("0.1.0")
	if p.Name() != "azure" {
		t.Errorf("Name() = %q, want azure", p.Name())
	}
}

func TestAzureProvider_Version(t *testing.T) {
	p := New("1.2.3")
	if p.Version() != "1.2.3" {
		t.Errorf("Version() = %q, want 1.2.3", p.Version())
	}
}

func TestAzureProvider_Manifest(t *testing.T) {
	p := New("0.1.0")
	m := p.Manifest()
	if m.Name != "azure" {
		t.Errorf("Manifest().Name = %q, want azure", m.Name)
	}
}

func TestAzureProvider_Capabilities(t *testing.T) {
	p := New("0.1.0")
	caps := p.Capabilities()
	if len(caps) != 13 {
		t.Errorf("Capabilities() returned %d, want 13", len(caps))
	}

	typeSet := make(map[string]bool)
	for _, c := range caps {
		typeSet[c.ResourceType] = true
	}
	expected := []string{
		"infra.container_service",
		"infra.k8s_cluster",
		"infra.database",
		"infra.cache",
		"infra.vpc",
		"infra.load_balancer",
		"infra.dns",
		"infra.registry",
		"infra.api_gateway",
		"infra.firewall",
		"infra.iam_role",
		"infra.storage",
		"infra.certificate",
	}
	for _, rt := range expected {
		if !typeSet[rt] {
			t.Errorf("missing capability: %s", rt)
		}
	}
}

func TestAzureProvider_ResourceDriver_Uninitialized(t *testing.T) {
	p := New("0.1.0")
	_, err := p.ResourceDriver("infra.database")
	if err == nil {
		t.Fatal("expected error for uninitialised provider")
	}
}

func TestAzureProvider_Plan(t *testing.T) {
	p := New("0.1.0")
	desired := []interfaces.ResourceSpec{
		{Name: "my-db", Type: "infra.database"},
		{Name: "my-vpc", Type: "infra.vpc"},
	}
	plan, err := p.Plan(context.Background(), desired, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Errorf("plan.Actions = %d, want 2", len(plan.Actions))
	}
	for _, a := range plan.Actions {
		if a.Action != "create" {
			t.Errorf("action = %q, want create", a.Action)
		}
	}
}

func TestAzureProvider_Plan_WithExisting(t *testing.T) {
	p := New("0.1.0")
	desired := []interfaces.ResourceSpec{
		{Name: "my-vpc", Type: "infra.vpc"},
	}
	current := []interfaces.ResourceState{
		{Name: "my-vpc", Type: "infra.vpc"},
	}
	plan, err := p.Plan(context.Background(), desired, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Action != "update" {
		t.Errorf("action = %q, want update", plan.Actions[0].Action)
	}
}

func TestAzureProvider_ResolveSizing_VM(t *testing.T) {
	p := New("0.1.0")
	sizing, err := p.ResolveSizing("infra.k8s_cluster", interfaces.SizeM, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sizing.InstanceType != "Standard_D2s_v5" {
		t.Errorf("InstanceType = %q, want Standard_D2s_v5", sizing.InstanceType)
	}
}

func TestAzureProvider_ResolveSizing_Database(t *testing.T) {
	p := New("0.1.0")
	sizing, err := p.ResolveSizing("infra.database", interfaces.SizeS, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sizing.Specs["sku_name"] != "GP_Gen5_1" {
		t.Errorf("sku_name = %v, want GP_Gen5_1", sizing.Specs["sku_name"])
	}
	if sizing.Specs["vcores"] != 1 {
		t.Errorf("vcores = %v, want 1", sizing.Specs["vcores"])
	}
}

func TestAzureProvider_ResolveSizing_Cache(t *testing.T) {
	p := New("0.1.0")
	sizing, err := p.ResolveSizing("infra.cache", interfaces.SizeXS, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sizing.Specs["sku_name"] != "C0" {
		t.Errorf("sku_name = %v, want C0", sizing.Specs["sku_name"])
	}
}

func TestAzureProvider_ResolveSizing_WithHints(t *testing.T) {
	p := New("0.1.0")
	hints := &interfaces.ResourceHints{CPU: "4", Memory: "8Gi"}
	sizing, err := p.ResolveSizing("infra.k8s_cluster", interfaces.SizeS, hints)
	if err != nil {
		t.Fatal(err)
	}
	if sizing.Specs["cpu"] != "4" {
		t.Errorf("cpu = %v, want 4", sizing.Specs["cpu"])
	}
}

func TestAzureProvider_Close(t *testing.T) {
	p := New("0.1.0")
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}
