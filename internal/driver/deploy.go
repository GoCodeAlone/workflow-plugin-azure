package driver

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerinstance/armcontainerinstance/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
)

// ─── App Gateway client ───────────────────────────────────────────────────────

// AppGatewayClient is the narrow interface for Azure Application Gateway operations.
type AppGatewayClient interface {
	Get(ctx context.Context, resourceGroup, name string) (armnetwork.ApplicationGateway, error)
	CreateOrUpdate(ctx context.Context, resourceGroup, name string, gw armnetwork.ApplicationGateway) (armnetwork.ApplicationGateway, error)
}

type realAppGatewayClient struct {
	inner *armnetwork.ApplicationGatewaysClient
}

func (c *realAppGatewayClient) Get(ctx context.Context, rg, name string) (armnetwork.ApplicationGateway, error) {
	res, err := c.inner.Get(ctx, rg, name, nil)
	if err != nil {
		return armnetwork.ApplicationGateway{}, err
	}
	return res.ApplicationGateway, nil
}

func (c *realAppGatewayClient) CreateOrUpdate(ctx context.Context, rg, name string, gw armnetwork.ApplicationGateway) (armnetwork.ApplicationGateway, error) {
	poller, err := c.inner.BeginCreateOrUpdate(ctx, rg, name, gw, nil)
	if err != nil {
		return armnetwork.ApplicationGateway{}, err
	}
	res, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	if err != nil {
		return armnetwork.ApplicationGateway{}, err
	}
	return res.ApplicationGateway, nil
}

// ─── ACIDeployDriver ──────────────────────────────────────────────────────────

// ACIDeployDriver implements module.DeployDriver for Azure Container Instances.
// It manages a single named container group and updates its image on Deploy.
type ACIDeployDriver struct {
	resourceGroup string
	location      string
	name          string
	client        ACIClient
}

// NewACIDeployDriver creates a DeployDriver backed by an ACI container group.
func NewACIDeployDriver(resourceGroup, location, name string, client ACIClient) *ACIDeployDriver {
	return &ACIDeployDriver{resourceGroup: resourceGroup, location: location, name: name, client: client}
}

func (d *ACIDeployDriver) Update(ctx context.Context, image string) error {
	cg, err := d.client.Get(ctx, d.resourceGroup, d.name)
	if err != nil {
		return fmt.Errorf("aci deploy: get %q: %w", d.name, err)
	}
	for _, c := range cg.Properties.Containers {
		if c.Properties != nil {
			c.Properties.Image = str(image)
		}
	}
	if _, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, d.name, cg); err != nil {
		return fmt.Errorf("aci deploy: update %q: %w", d.name, err)
	}
	return nil
}

func (d *ACIDeployDriver) HealthCheck(ctx context.Context, _ string) error {
	cg, err := d.client.Get(ctx, d.resourceGroup, d.name)
	if err != nil {
		return fmt.Errorf("aci deploy: health check %q: %w", d.name, err)
	}
	state := ""
	if cg.Properties != nil {
		state = strVal(cg.Properties.ProvisioningState)
	}
	if state != "Succeeded" {
		return fmt.Errorf("aci deploy: %q provisioning state %q", d.name, state)
	}
	return nil
}

func (d *ACIDeployDriver) CurrentImage(ctx context.Context) (string, error) {
	cg, err := d.client.Get(ctx, d.resourceGroup, d.name)
	if err != nil {
		return "", fmt.Errorf("aci deploy: current image %q: %w", d.name, err)
	}
	if cg.Properties == nil || len(cg.Properties.Containers) == 0 {
		return "", fmt.Errorf("aci deploy: no containers in %q", d.name)
	}
	return strVal(cg.Properties.Containers[0].Properties.Image), nil
}

// ReplicaCount returns 1; ACI container groups do not natively support replicas.
func (d *ACIDeployDriver) ReplicaCount(_ context.Context) (int, error) {
	return 1, nil
}

// ─── ACIBlueGreenDriver ───────────────────────────────────────────────────────

// ACIBlueGreenDriver implements module.BlueGreenDriver using two ACI container
// groups and an Azure Application Gateway for traffic switching.
//
// Blue environment: container group named [name].
// Green environment: container group named [name]-green.
//
// SwitchTraffic updates the App Gateway backend pool to the green IP. If
// agwClient is nil, SwitchTraffic is a no-op (useful in environments that
// front ACI with DNS instead of App Gateway).
type ACIBlueGreenDriver struct {
	resourceGroup  string
	location       string
	blueName       string
	greenName      string
	client         ACIClient
	agwClient      AppGatewayClient // may be nil
	agwName        string
	agwBackendPool string
	greenIP        string // cached after CreateGreen
}

// NewACIBlueGreenDriver creates a BlueGreenDriver for ACI.
// Pass a non-nil agwClient with agwName/agwBackendPool to enable App Gateway traffic switching.
func NewACIBlueGreenDriver(resourceGroup, location, name string, client ACIClient, agwClient AppGatewayClient, agwName, agwBackendPool string) *ACIBlueGreenDriver {
	return &ACIBlueGreenDriver{
		resourceGroup:  resourceGroup,
		location:       location,
		blueName:       name,
		greenName:      name + "-green",
		client:         client,
		agwClient:      agwClient,
		agwName:        agwName,
		agwBackendPool: agwBackendPool,
	}
}

// DeployDriver methods delegate to the blue (stable) container group.

func (d *ACIBlueGreenDriver) Update(ctx context.Context, image string) error {
	blue := NewACIDeployDriver(d.resourceGroup, d.location, d.blueName, d.client)
	return blue.Update(ctx, image)
}

func (d *ACIBlueGreenDriver) HealthCheck(ctx context.Context, path string) error {
	green := NewACIDeployDriver(d.resourceGroup, d.location, d.greenName, d.client)
	return green.HealthCheck(ctx, path)
}

func (d *ACIBlueGreenDriver) CurrentImage(ctx context.Context) (string, error) {
	blue := NewACIDeployDriver(d.resourceGroup, d.location, d.blueName, d.client)
	return blue.CurrentImage(ctx)
}

func (d *ACIBlueGreenDriver) ReplicaCount(_ context.Context) (int, error) {
	return 1, nil
}

// CreateGreen creates a new container group with the "-green" suffix and the given image.
func (d *ACIBlueGreenDriver) CreateGreen(ctx context.Context, image string) error {
	blue, err := d.client.Get(ctx, d.resourceGroup, d.blueName)
	if err != nil {
		return fmt.Errorf("aci blue-green: get blue %q: %w", d.blueName, err)
	}

	// Copy blue's spec and override image.
	greenCG := armcontainerinstance.ContainerGroup{
		Location:   blue.Location,
		Properties: blue.Properties,
	}
	if greenCG.Properties != nil {
		for _, c := range greenCG.Properties.Containers {
			if c.Properties != nil {
				c.Properties.Image = str(image)
			}
		}
	}

	result, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, d.greenName, greenCG)
	if err != nil {
		return fmt.Errorf("aci blue-green: create green %q: %w", d.greenName, err)
	}
	if result.Properties != nil && result.Properties.IPAddress != nil {
		d.greenIP = strVal(result.Properties.IPAddress.IP)
	}
	return nil
}

// SwitchTraffic updates the App Gateway backend pool to route traffic to the green container group.
// If no agwClient is configured, this is a no-op.
func (d *ACIBlueGreenDriver) SwitchTraffic(ctx context.Context) error {
	if d.agwClient == nil {
		return nil
	}
	gw, err := d.agwClient.Get(ctx, d.resourceGroup, d.agwName)
	if err != nil {
		return fmt.Errorf("aci blue-green: get app gateway %q: %w", d.agwName, err)
	}
	if gw.Properties == nil {
		return fmt.Errorf("aci blue-green: app gateway %q has no properties", d.agwName)
	}
	for _, pool := range gw.Properties.BackendAddressPools {
		if pool.Name != nil && *pool.Name == d.agwBackendPool && pool.Properties != nil {
			pool.Properties.BackendAddresses = []*armnetwork.ApplicationGatewayBackendAddress{
				{IPAddress: str(d.greenIP)},
			}
		}
	}
	if _, err := d.agwClient.CreateOrUpdate(ctx, d.resourceGroup, d.agwName, gw); err != nil {
		return fmt.Errorf("aci blue-green: update app gateway %q: %w", d.agwName, err)
	}
	return nil
}

// DestroyBlue deletes the old (blue) container group.
func (d *ACIBlueGreenDriver) DestroyBlue(ctx context.Context) error {
	if err := d.client.Delete(ctx, d.resourceGroup, d.blueName); err != nil {
		return fmt.Errorf("aci blue-green: destroy blue %q: %w", d.blueName, err)
	}
	return nil
}

// GreenEndpoint returns the IP address of the green container group.
func (d *ACIBlueGreenDriver) GreenEndpoint(_ context.Context) (string, error) {
	if d.greenIP == "" {
		return "", fmt.Errorf("aci blue-green: green endpoint not available (CreateGreen not called or no public IP)")
	}
	return d.greenIP, nil
}

// ─── ACICanaryDriver ──────────────────────────────────────────────────────────

// ACICanaryDriver implements module.CanaryDriver using ACI + App Gateway weighted
// backend pools. Azure Application Gateway supports weighted round-robin via
// multiple backend pools, but native percentage-based traffic splitting is not
// available without Azure Front Door or Traffic Manager.
//
// RoutePercent documents this limitation and returns an error directing users to
// Azure Front Door for canary deployments requiring fine-grained traffic control.
type ACICanaryDriver struct {
	resourceGroup string
	location      string
	stableName    string
	canaryName    string
	client        ACIClient
	agwClient     AppGatewayClient
	agwName       string
}

// NewACICanaryDriver creates a CanaryDriver for ACI.
func NewACICanaryDriver(resourceGroup, location, name string, client ACIClient, agwClient AppGatewayClient, agwName string) *ACICanaryDriver {
	return &ACICanaryDriver{
		resourceGroup: resourceGroup,
		location:      location,
		stableName:    name,
		canaryName:    name + "-canary",
		client:        client,
		agwClient:     agwClient,
		agwName:       agwName,
	}
}

// DeployDriver methods delegate to the stable container group.

func (d *ACICanaryDriver) Update(ctx context.Context, image string) error {
	stable := NewACIDeployDriver(d.resourceGroup, d.location, d.stableName, d.client)
	return stable.Update(ctx, image)
}

func (d *ACICanaryDriver) HealthCheck(ctx context.Context, path string) error {
	canary := NewACIDeployDriver(d.resourceGroup, d.location, d.canaryName, d.client)
	return canary.HealthCheck(ctx, path)
}

func (d *ACICanaryDriver) CurrentImage(ctx context.Context) (string, error) {
	stable := NewACIDeployDriver(d.resourceGroup, d.location, d.stableName, d.client)
	return stable.CurrentImage(ctx)
}

func (d *ACICanaryDriver) ReplicaCount(_ context.Context) (int, error) {
	return 1, nil
}

// CreateCanary creates a new ACI container group for the canary instance.
func (d *ACICanaryDriver) CreateCanary(ctx context.Context, image string) error {
	stable, err := d.client.Get(ctx, d.resourceGroup, d.stableName)
	if err != nil {
		return fmt.Errorf("aci canary: get stable %q: %w", d.stableName, err)
	}
	canaryCG := armcontainerinstance.ContainerGroup{
		Location:   stable.Location,
		Properties: stable.Properties,
	}
	if canaryCG.Properties != nil {
		for _, c := range canaryCG.Properties.Containers {
			if c.Properties != nil {
				c.Properties.Image = str(image)
			}
		}
	}
	if _, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, d.canaryName, canaryCG); err != nil {
		return fmt.Errorf("aci canary: create canary %q: %w", d.canaryName, err)
	}
	return nil
}

// RoutePercent is not natively supported by Azure Application Gateway for ACI
// backends. Azure Front Door or Traffic Manager is required for
// percentage-based canary traffic splitting. Returns an unsupported error.
func (d *ACICanaryDriver) RoutePercent(_ context.Context, percent int) error {
	return fmt.Errorf("aci canary: RoutePercent(%d) unsupported — Azure Application Gateway does not "+
		"natively support percentage-based traffic splitting for ACI; use Azure Front Door or Traffic Manager", percent)
}

// CheckMetricGate always passes (no native metric integration).
func (d *ACICanaryDriver) CheckMetricGate(_ context.Context, gate string) error {
	return nil
}

// PromoteCanary updates the stable container group with the canary image and
// removes the canary instance.
func (d *ACICanaryDriver) PromoteCanary(ctx context.Context) error {
	canaryImg, err := NewACIDeployDriver(d.resourceGroup, d.location, d.canaryName, d.client).CurrentImage(ctx)
	if err != nil {
		return fmt.Errorf("aci canary: get canary image: %w", err)
	}
	if err := NewACIDeployDriver(d.resourceGroup, d.location, d.stableName, d.client).Update(ctx, canaryImg); err != nil {
		return fmt.Errorf("aci canary: promote to stable: %w", err)
	}
	return d.DestroyCanary(ctx)
}

// DestroyCanary deletes the canary container group.
func (d *ACICanaryDriver) DestroyCanary(ctx context.Context) error {
	if err := d.client.Delete(ctx, d.resourceGroup, d.canaryName); err != nil {
		return fmt.Errorf("aci canary: destroy canary %q: %w", d.canaryName, err)
	}
	return nil
}
