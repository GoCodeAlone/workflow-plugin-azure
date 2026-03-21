// Package driver implements Azure resource drivers for the IaC provider.
// Each driver manages the lifecycle of a specific Azure resource type and
// implements interfaces.ResourceDriver.
package driver

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/apimanagement/armapimanagement"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerinstance/armcontainerinstance/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerregistry/armcontainerregistry"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/redis/armredis"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// NewAll creates all Azure resource drivers and returns them keyed by resource type.
func NewAll(subscriptionID, resourceGroup, location string, cred azcore.TokenCredential) (map[string]interfaces.ResourceDriver, error) {
	aciRaw, err := armcontainerinstance.NewContainerGroupsClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("aci client: %w", err)
	}

	aksRaw, err := armcontainerservice.NewManagedClustersClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("aks client: %w", err)
	}

	sqlSrvRaw, err := armsql.NewServersClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("sql servers client: %w", err)
	}
	sqlDBRaw, err := armsql.NewDatabasesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("sql databases client: %w", err)
	}

	redisRaw, err := armredis.NewClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("redis client: %w", err)
	}

	vnetRaw, err := armnetwork.NewVirtualNetworksClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("vnet client: %w", err)
	}

	lbRaw, err := armnetwork.NewLoadBalancersClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("lb client: %w", err)
	}

	dnsRaw, err := armdns.NewZonesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("dns client: %w", err)
	}

	acrRaw, err := armcontainerregistry.NewRegistriesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("acr client: %w", err)
	}

	apimRaw, err := armapimanagement.NewServiceClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("apim client: %w", err)
	}

	nsgRaw, err := armnetwork.NewSecurityGroupsClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("nsg client: %w", err)
	}

	msiRaw, err := armmsi.NewUserAssignedIdentitiesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("msi client: %w", err)
	}

	blobRaw, err := azblob.NewClient(
		fmt.Sprintf("https://placeholder.blob.core.windows.net/"),
		cred, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("blob client: %w", err)
	}

	certRaw, err := armappservice.NewCertificatesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("cert client: %w", err)
	}

	return map[string]interfaces.ResourceDriver{
		"infra.container_service": NewACIDriver(resourceGroup, location, &realACIClient{inner: aciRaw}),
		"infra.k8s_cluster":       NewAKSDriver(resourceGroup, location, &realAKSClient{inner: aksRaw}),
		"infra.database":          NewSQLDriver(resourceGroup, location, &realSQLClient{servers: sqlSrvRaw, databases: sqlDBRaw}),
		"infra.cache":             NewRedisDriver(resourceGroup, location, &realRedisClient{inner: redisRaw}),
		"infra.vpc":               NewVNetDriver(resourceGroup, location, &realVNetClient{inner: vnetRaw}),
		"infra.load_balancer":     NewLBDriver(resourceGroup, location, &realLBClient{inner: lbRaw}),
		"infra.dns":               NewDNSDriver(resourceGroup, location, &realDNSClient{inner: dnsRaw}),
		"infra.registry":          NewACRDriver(resourceGroup, location, &realACRClient{inner: acrRaw}),
		"infra.api_gateway":       NewAPIMDriver(resourceGroup, location, &realAPIMClient{inner: apimRaw}),
		"infra.firewall":          NewNSGDriver(resourceGroup, location, &realNSGClient{inner: nsgRaw}),
		"infra.iam_role":          NewMSIDriver(resourceGroup, location, &realMSIClient{inner: msiRaw}),
		"infra.storage":           NewBlobDriver(resourceGroup, location, &realBlobClient{inner: blobRaw}),
		"infra.certificate":       NewCertDriver(resourceGroup, location, &realCertClient{inner: certRaw}),
	}, nil
}

// str returns a pointer to a string.
func str(s string) *string { return &s }

// strVal safely dereferences a string pointer.
func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// configStr extracts a string config value with a fallback default.
func configStr(config map[string]any, key, defaultVal string) string {
	if v, ok := config[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

// configInt extracts an int config value with a fallback default.
func configInt(config map[string]any, key string, defaultVal int) int {
	switch v := config[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return defaultVal
}
