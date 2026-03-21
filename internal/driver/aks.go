package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// AKSClient is the narrow interface for Azure Kubernetes Service operations.
type AKSClient interface {
	CreateOrUpdate(ctx context.Context, resourceGroup, name string, cluster armcontainerservice.ManagedCluster) (armcontainerservice.ManagedCluster, error)
	Get(ctx context.Context, resourceGroup, name string) (armcontainerservice.ManagedCluster, error)
	Delete(ctx context.Context, resourceGroup, name string) error
}

type realAKSClient struct {
	inner *armcontainerservice.ManagedClustersClient
}

func (c *realAKSClient) CreateOrUpdate(ctx context.Context, rg, name string, cluster armcontainerservice.ManagedCluster) (armcontainerservice.ManagedCluster, error) {
	poller, err := c.inner.BeginCreateOrUpdate(ctx, rg, name, cluster, nil)
	if err != nil {
		return armcontainerservice.ManagedCluster{}, err
	}
	res, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	if err != nil {
		return armcontainerservice.ManagedCluster{}, err
	}
	return res.ManagedCluster, nil
}

func (c *realAKSClient) Get(ctx context.Context, rg, name string) (armcontainerservice.ManagedCluster, error) {
	res, err := c.inner.Get(ctx, rg, name, nil)
	if err != nil {
		return armcontainerservice.ManagedCluster{}, err
	}
	return res.ManagedCluster, nil
}

func (c *realAKSClient) Delete(ctx context.Context, rg, name string) error {
	poller, err := c.inner.BeginDelete(ctx, rg, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	return err
}

// AKSDriver manages Azure Kubernetes Service clusters (infra.k8s_cluster).
type AKSDriver struct {
	resourceGroup string
	location      string
	client        AKSClient
}

var _ interfaces.ResourceDriver = (*AKSDriver)(nil)

func NewAKSDriver(resourceGroup, location string, client AKSClient) *AKSDriver {
	return &AKSDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *AKSDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	k8sVersion := configStr(spec.Config, "kubernetes_version", "1.30")
	nodeCount := int32(configInt(spec.Config, "node_count", 2))
	vmSize := configStr(spec.Config, "vm_size", "Standard_D2s_v5")

	cluster := armcontainerservice.ManagedCluster{
		Location: str(d.location),
		Identity: &armcontainerservice.ManagedClusterIdentity{
			Type: ptrOf(armcontainerservice.ResourceIdentityTypeSystemAssigned),
		},
		Properties: &armcontainerservice.ManagedClusterProperties{
			KubernetesVersion: str(k8sVersion),
			DNSPrefix:         str(spec.Name),
			AgentPoolProfiles: []*armcontainerservice.ManagedClusterAgentPoolProfile{
				{
					Name:   str("default"),
					Count:  &nodeCount,
					VMSize: str(vmSize),
					Mode:   ptrOf(armcontainerservice.AgentPoolModeSystem),
				},
			},
		},
	}

	result, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, spec.Name, cluster)
	if err != nil {
		return nil, fmt.Errorf("aks: create %q: %w", spec.Name, err)
	}
	return aksToOutput(spec.Name, result), nil
}

func (d *AKSDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	result, err := d.client.Get(ctx, d.resourceGroup, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("aks: get %q: %w", ref.Name, err)
	}
	return aksToOutput(ref.Name, result), nil
}

func (d *AKSDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *AKSDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.Delete(ctx, d.resourceGroup, ref.Name)
}

func (d *AKSDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	var changes []interfaces.FieldChange
	if desiredVer, ok := desired.Config["kubernetes_version"].(string); ok {
		if cur, ok := current.Outputs["kubernetes_version"].(string); ok && desiredVer != cur {
			changes = append(changes, interfaces.FieldChange{Path: "kubernetes_version", Old: cur, New: desiredVer})
		}
	}
	return &interfaces.DiffResult{NeedsUpdate: len(changes) > 0, Changes: changes}, nil
}

func (d *AKSDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	healthy := out.Status == "Succeeded"
	return &interfaces.HealthResult{Healthy: healthy, Message: out.Status}, nil
}

func (d *AKSDriver) Scale(ctx context.Context, ref interfaces.ResourceRef, replicas int) (*interfaces.ResourceOutput, error) {
	cur, err := d.client.Get(ctx, d.resourceGroup, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("aks: scale get %q: %w", ref.Name, err)
	}
	count := int32(replicas)
	if len(cur.Properties.AgentPoolProfiles) > 0 {
		cur.Properties.AgentPoolProfiles[0].Count = &count
	}
	result, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, ref.Name, cur)
	if err != nil {
		return nil, fmt.Errorf("aks: scale %q: %w", ref.Name, err)
	}
	return aksToOutput(ref.Name, result), nil
}

func aksToOutput(name string, c armcontainerservice.ManagedCluster) *interfaces.ResourceOutput {
	status := "unknown"
	outputs := map[string]any{}
	if c.Properties != nil {
		if c.Properties.ProvisioningState != nil {
			status = strVal(c.Properties.ProvisioningState)
		}
		if c.Properties.KubernetesVersion != nil {
			outputs["kubernetes_version"] = strVal(c.Properties.KubernetesVersion)
		}
		if c.Properties.Fqdn != nil {
			outputs["fqdn"] = strVal(c.Properties.Fqdn)
		}
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.k8s_cluster",
		ProviderID: strVal(c.ID),
		Outputs:    outputs,
		Status:     status,
	}
}
