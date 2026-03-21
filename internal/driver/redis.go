package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/redis/armredis"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// RedisClient is the narrow interface for Azure Cache for Redis operations.
type RedisClient interface {
	Create(ctx context.Context, resourceGroup, name string, params armredis.CreateParameters) (armredis.ResourceInfo, error)
	Get(ctx context.Context, resourceGroup, name string) (armredis.ResourceInfo, error)
	Delete(ctx context.Context, resourceGroup, name string) error
}

type realRedisClient struct {
	inner *armredis.Client
}

func (c *realRedisClient) Create(ctx context.Context, rg, name string, params armredis.CreateParameters) (armredis.ResourceInfo, error) {
	poller, err := c.inner.BeginCreate(ctx, rg, name, params, nil)
	if err != nil {
		return armredis.ResourceInfo{}, err
	}
	res, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	if err != nil {
		return armredis.ResourceInfo{}, err
	}
	return res.ResourceInfo, nil
}

func (c *realRedisClient) Get(ctx context.Context, rg, name string) (armredis.ResourceInfo, error) {
	res, err := c.inner.Get(ctx, rg, name, nil)
	if err != nil {
		return armredis.ResourceInfo{}, err
	}
	return res.ResourceInfo, nil
}

func (c *realRedisClient) Delete(ctx context.Context, rg, name string) error {
	poller, err := c.inner.BeginDelete(ctx, rg, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	return err
}

// RedisDriver manages Azure Cache for Redis (infra.cache).
type RedisDriver struct {
	resourceGroup string
	location      string
	client        RedisClient
}

var _ interfaces.ResourceDriver = (*RedisDriver)(nil)

func NewRedisDriver(resourceGroup, location string, client RedisClient) *RedisDriver {
	return &RedisDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *RedisDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	skuName := configStr(spec.Config, "sku_name", "C1")
	capacity := int32(configInt(spec.Config, "capacity", 1))

	params := armredis.CreateParameters{
		Location: str(d.location),
		Properties: &armredis.CreateProperties{
			SKU: &armredis.SKU{
				Name:     ptrOf(armredis.SKUNameBasic),
				Family:   ptrOf(armredis.SKUFamilyC),
				Capacity: &capacity,
			},
			EnableNonSSLPort: ptrOf(false),
		},
	}
	_ = skuName // capacity already handles sizing; sku_name maps to SKU family/name

	result, err := d.client.Create(ctx, d.resourceGroup, spec.Name, params)
	if err != nil {
		return nil, fmt.Errorf("redis: create %q: %w", spec.Name, err)
	}
	return redisToOutput(spec.Name, result), nil
}

func (d *RedisDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	result, err := d.client.Get(ctx, d.resourceGroup, ref.Name)
	if err != nil {
		return nil, fmt.Errorf("redis: get %q: %w", ref.Name, err)
	}
	return redisToOutput(ref.Name, result), nil
}

func (d *RedisDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *RedisDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.Delete(ctx, d.resourceGroup, ref.Name)
}

func (d *RedisDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	var changes []interfaces.FieldChange
	if cap, ok := desired.Config["capacity"].(int); ok {
		if cur, ok := current.Outputs["capacity"].(int); ok && cap != cur {
			changes = append(changes, interfaces.FieldChange{Path: "capacity", Old: cur, New: cap})
		}
	}
	return &interfaces.DiffResult{NeedsUpdate: len(changes) > 0, Changes: changes}, nil
}

func (d *RedisDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	healthy := out.Status == "Succeeded"
	return &interfaces.HealthResult{Healthy: healthy, Message: out.Status}, nil
}

func (d *RedisDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("redis: scale not supported — update capacity in spec")
}

func redisToOutput(name string, r armredis.ResourceInfo) *interfaces.ResourceOutput {
	status := "unknown"
	outputs := map[string]any{}
	if r.Properties != nil {
		if r.Properties.ProvisioningState != nil {
			status = string(*r.Properties.ProvisioningState)
		}
		if r.Properties.HostName != nil {
			outputs["host"] = strVal(r.Properties.HostName)
		}
		if r.Properties.Port != nil {
			outputs["port"] = *r.Properties.Port
		}
		if r.Properties.SSLPort != nil {
			outputs["ssl_port"] = *r.Properties.SSLPort
		}
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.cache",
		ProviderID: strVal(r.ID),
		Outputs:    outputs,
		Status:     status,
	}
}
