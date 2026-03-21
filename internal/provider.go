// Package internal implements the Azure IaC provider for the workflow engine.
package internal

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/GoCodeAlone/workflow-plugin-azure/internal/driver"
	"github.com/GoCodeAlone/workflow/interfaces"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

// AzureProvider implements interfaces.IaCProvider for Microsoft Azure.
type AzureProvider struct {
	mu             sync.RWMutex
	version        string
	subscriptionID string
	resourceGroup  string
	location       string
	drivers        map[string]interfaces.ResourceDriver
}

// Ensure AzureProvider satisfies both sdk.PluginProvider and interfaces.IaCProvider.
var _ sdk.PluginProvider = (*AzureProvider)(nil)
var _ interfaces.IaCProvider = (*AzureProvider)(nil)

// New creates a new AzureProvider with the given version string.
func New(version string) *AzureProvider {
	return &AzureProvider{
		version: version,
		drivers: make(map[string]interfaces.ResourceDriver),
	}
}

// Manifest implements sdk.PluginProvider.
func (p *AzureProvider) Manifest() sdk.PluginManifest {
	return sdk.PluginManifest{
		Name:        "azure",
		Version:     p.version,
		Author:      "GoCodeAlone",
		Description: "Microsoft Azure infrastructure provider (ACI, AKS, SQL, Redis, VNet, LB, DNS, ACR, APIM, NSG, MSI, Blob, Certificates)",
	}
}

// Name implements interfaces.IaCProvider.
func (p *AzureProvider) Name() string { return "azure" }

// Version implements interfaces.IaCProvider.
func (p *AzureProvider) Version() string { return p.version }

// Initialize configures the Azure provider from the given config map.
// Expected keys: subscription_id, resource_group, location.
// Credentials are resolved via azidentity.NewDefaultAzureCredential.
func (p *AzureProvider) Initialize(ctx context.Context, config map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	subID, _ := config["subscription_id"].(string)
	if subID == "" {
		return fmt.Errorf("azure: subscription_id is required")
	}
	rg, _ := config["resource_group"].(string)
	if rg == "" {
		return fmt.Errorf("azure: resource_group is required")
	}
	loc, _ := config["location"].(string)
	if loc == "" {
		loc = "eastus"
	}

	p.subscriptionID = subID
	p.resourceGroup = rg
	p.location = loc

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("azure: credential: %w", err)
	}

	drivers, err := driver.NewAll(subID, rg, loc, cred)
	if err != nil {
		return fmt.Errorf("azure: init drivers: %w", err)
	}
	p.drivers = drivers
	return nil
}

// Capabilities returns the resource types this provider supports.
func (p *AzureProvider) Capabilities() []interfaces.CapabilityDeclaration {
	return []interfaces.CapabilityDeclaration{
		{ResourceType: "infra.container_service", Tier: 1, Operations: []string{"create", "read", "update", "delete", "scale"}},
		{ResourceType: "infra.k8s_cluster", Tier: 1, Operations: []string{"create", "read", "update", "delete", "scale"}},
		{ResourceType: "infra.database", Tier: 1, Operations: []string{"create", "read", "update", "delete"}},
		{ResourceType: "infra.cache", Tier: 1, Operations: []string{"create", "read", "update", "delete"}},
		{ResourceType: "infra.vpc", Tier: 1, Operations: []string{"create", "read", "update", "delete"}},
		{ResourceType: "infra.load_balancer", Tier: 1, Operations: []string{"create", "read", "update", "delete"}},
		{ResourceType: "infra.dns", Tier: 1, Operations: []string{"create", "read", "update", "delete"}},
		{ResourceType: "infra.registry", Tier: 1, Operations: []string{"create", "read", "update", "delete"}},
		{ResourceType: "infra.api_gateway", Tier: 2, Operations: []string{"create", "read", "update", "delete"}},
		{ResourceType: "infra.firewall", Tier: 1, Operations: []string{"create", "read", "update", "delete"}},
		{ResourceType: "infra.iam_role", Tier: 1, Operations: []string{"create", "read", "update", "delete"}},
		{ResourceType: "infra.storage", Tier: 1, Operations: []string{"create", "read", "update", "delete"}},
		{ResourceType: "infra.certificate", Tier: 2, Operations: []string{"create", "read", "update", "delete"}},
	}
}

// Plan compares desired state with current state and produces an execution plan.
func (p *AzureProvider) Plan(_ context.Context, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (*interfaces.Plan, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	currentByName := make(map[string]interfaces.ResourceState, len(current))
	for _, s := range current {
		currentByName[s.Name] = s
	}

	var actions []interfaces.PlanAction
	for _, spec := range desired {
		cur, exists := currentByName[spec.Name]
		if !exists {
			actions = append(actions, interfaces.PlanAction{
				Action:   "create",
				Resource: spec,
			})
		} else {
			actions = append(actions, interfaces.PlanAction{
				Action:   "update",
				Resource: spec,
				Current:  &cur,
			})
		}
	}

	return &interfaces.Plan{
		ID:        fmt.Sprintf("plan-%d", time.Now().UnixNano()),
		Actions:   actions,
		CreatedAt: time.Now(),
	}, nil
}

// Apply executes the given plan by delegating to resource drivers.
func (p *AzureProvider) Apply(ctx context.Context, plan *interfaces.Plan) (*interfaces.ApplyResult, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := &interfaces.ApplyResult{PlanID: plan.ID}
	for _, action := range plan.Actions {
		drv, err := p.resourceDriver(action.Resource.Type)
		if err != nil {
			result.Errors = append(result.Errors, interfaces.ActionError{
				Resource: action.Resource.Name,
				Action:   action.Action,
				Error:    err.Error(),
			})
			continue
		}

		var out *interfaces.ResourceOutput
		switch action.Action {
		case "create":
			out, err = drv.Create(ctx, action.Resource)
		case "update":
			ref := interfaces.ResourceRef{Name: action.Resource.Name, Type: action.Resource.Type}
			if action.Current != nil {
				ref.ProviderID = action.Current.ProviderID
			}
			out, err = drv.Update(ctx, ref, action.Resource)
		case "delete":
			ref := interfaces.ResourceRef{Name: action.Resource.Name, Type: action.Resource.Type}
			if action.Current != nil {
				ref.ProviderID = action.Current.ProviderID
			}
			err = drv.Delete(ctx, ref)
		}

		if err != nil {
			result.Errors = append(result.Errors, interfaces.ActionError{
				Resource: action.Resource.Name,
				Action:   action.Action,
				Error:    err.Error(),
			})
			continue
		}
		if out != nil {
			result.Resources = append(result.Resources, *out)
		}
	}
	return result, nil
}

// Destroy deletes a set of resources by calling each driver's Delete method.
func (p *AzureProvider) Destroy(ctx context.Context, resources []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := &interfaces.DestroyResult{}
	for _, ref := range resources {
		drv, err := p.resourceDriver(ref.Type)
		if err != nil {
			result.Errors = append(result.Errors, interfaces.ActionError{
				Resource: ref.Name,
				Action:   "delete",
				Error:    err.Error(),
			})
			continue
		}
		if err := drv.Delete(ctx, ref); err != nil {
			result.Errors = append(result.Errors, interfaces.ActionError{
				Resource: ref.Name,
				Action:   "delete",
				Error:    err.Error(),
			})
			continue
		}
		result.Destroyed = append(result.Destroyed, ref.Name)
	}
	return result, nil
}

// Status returns the live status of each resource by calling each driver's Read method.
func (p *AzureProvider) Status(ctx context.Context, resources []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var statuses []interfaces.ResourceStatus
	for _, ref := range resources {
		drv, err := p.resourceDriver(ref.Type)
		if err != nil {
			statuses = append(statuses, interfaces.ResourceStatus{
				Name: ref.Name, Type: ref.Type, Status: "unknown",
			})
			continue
		}
		out, err := drv.Read(ctx, ref)
		if err != nil {
			statuses = append(statuses, interfaces.ResourceStatus{
				Name: ref.Name, Type: ref.Type, Status: "unknown",
			})
			continue
		}
		statuses = append(statuses, interfaces.ResourceStatus{
			Name:       out.Name,
			Type:       out.Type,
			ProviderID: out.ProviderID,
			Status:     out.Status,
			Outputs:    out.Outputs,
		})
	}
	return statuses, nil
}

// DetectDrift checks whether actual provider state differs from desired state.
func (p *AzureProvider) DetectDrift(ctx context.Context, resources []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var results []interfaces.DriftResult
	for _, ref := range resources {
		drv, err := p.resourceDriver(ref.Type)
		if err != nil {
			results = append(results, interfaces.DriftResult{
				Name: ref.Name, Type: ref.Type, Drifted: false,
			})
			continue
		}
		out, err := drv.Read(ctx, ref)
		if err != nil {
			results = append(results, interfaces.DriftResult{
				Name: ref.Name, Type: ref.Type, Drifted: false,
			})
			continue
		}
		results = append(results, interfaces.DriftResult{
			Name:   ref.Name,
			Type:   ref.Type,
			Actual: out.Outputs,
		})
	}
	return results, nil
}

// Import imports an existing Azure resource into managed state.
func (p *AzureProvider) Import(ctx context.Context, cloudID string, resourceType string) (*interfaces.ResourceState, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	drv, err := p.resourceDriver(resourceType)
	if err != nil {
		return nil, err
	}
	ref := interfaces.ResourceRef{Name: cloudID, Type: resourceType, ProviderID: cloudID}
	out, err := drv.Read(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("azure: import %s/%s: %w", resourceType, cloudID, err)
	}
	return &interfaces.ResourceState{
		ID:         out.ProviderID,
		Name:       out.Name,
		Type:       out.Type,
		Provider:   "azure",
		ProviderID: out.ProviderID,
		Outputs:    out.Outputs,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}, nil
}

// ResolveSizing maps an abstract size tier to Azure-specific sizing.
func (p *AzureProvider) ResolveSizing(resourceType string, size interfaces.Size, hints *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	instanceType := resolveInstanceType(size)
	specs := map[string]any{"instance_type": instanceType}

	switch resourceType {
	case "infra.database":
		tier := dbSizing[size]
		specs["edition"] = tier.edition
		specs["service_tier"] = tier.serviceTier
		specs["dtu"] = tier.dtu
		if tier.vCores > 0 {
			specs["vcores"] = tier.vCores
		}
	case "infra.cache":
		specs["sku_name"] = cacheSizing[size]
		delete(specs, "instance_type")
	}

	if hints != nil {
		if hints.CPU != "" {
			specs["cpu"] = hints.CPU
		}
		if hints.Memory != "" {
			specs["memory"] = hints.Memory
		}
		if hints.Storage != "" {
			specs["storage"] = hints.Storage
		}
	}

	return &interfaces.ProviderSizing{
		InstanceType: instanceType,
		Specs:        specs,
	}, nil
}

// ResourceDriver returns the driver for the given resource type.
func (p *AzureProvider) ResourceDriver(resourceType string) (interfaces.ResourceDriver, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.resourceDriver(resourceType)
}

// resourceDriver is the unlocked internal version.
func (p *AzureProvider) resourceDriver(resourceType string) (interfaces.ResourceDriver, error) {
	drv, ok := p.drivers[resourceType]
	if !ok {
		return nil, fmt.Errorf("azure: unsupported resource type: %s", resourceType)
	}
	return drv, nil
}

// Close releases provider resources.
func (p *AzureProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.drivers = make(map[string]interfaces.ResourceDriver)
	return nil
}
