package internal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/GoCodeAlone/workflow/interfaces"
)

const (
	ownershipTagKey    = "workflow-owner"
	ownershipTagSource = "tag:workflow-owner"
)

var ErrOwnershipARMIDRequired = errors.New("azure ownership requires ResourceRef.ProviderID to be an ARM resource ID")

type ownershipTagsClient interface {
	GetAtScope(context.Context, string) (armresources.TagsResource, error)
	UpdateAtScope(context.Context, string, armresources.TagsPatchResource) error
}

type ownershipResourcesClient interface {
	ListByResourceGroup(context.Context, string, string) ([]armresources.GenericResourceExpanded, error)
}

type azureOwnershipTagsClient struct {
	inner *armresources.TagsClient
}

func (c *azureOwnershipTagsClient) GetAtScope(ctx context.Context, scope string) (armresources.TagsResource, error) {
	resp, err := c.inner.GetAtScope(ctx, scope, nil)
	if err != nil {
		return armresources.TagsResource{}, err
	}
	return resp.TagsResource, nil
}

func (c *azureOwnershipTagsClient) UpdateAtScope(ctx context.Context, scope string, parameters armresources.TagsPatchResource) error {
	_, err := c.inner.UpdateAtScope(ctx, scope, parameters, nil)
	return err
}

type azureOwnershipResourcesClient struct {
	inner *armresources.Client
}

func (c *azureOwnershipResourcesClient) ListByResourceGroup(ctx context.Context, resourceGroup, filter string) ([]armresources.GenericResourceExpanded, error) {
	pager := c.inner.NewListByResourceGroupPager(resourceGroup, &armresources.ClientListByResourceGroupOptions{
		Filter: &filter,
	})
	var resources []armresources.GenericResourceExpanded
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, resource := range page.Value {
			if resource != nil {
				resources = append(resources, *resource)
			}
		}
	}
	return resources, nil
}

func (p *AzureProvider) GetOwner(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOwner, error) {
	p.mu.RLock()
	client, err := p.ownershipTagsClientLocked()
	p.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	scope, err := ownershipARMID(ref)
	if err != nil {
		return nil, err
	}
	tags, err := client.GetAtScope(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("azure: get ownership tags for %q: %w", ref.Name, err)
	}
	return &interfaces.ResourceOwner{Ref: ref, Owner: ownerFromAzureTags(tags.Properties), Source: ownershipTagSource}, nil
}

func (p *AzureProvider) SetOwner(ctx context.Context, ref interfaces.ResourceRef, owner string) error {
	if strings.TrimSpace(owner) == "" {
		return fmt.Errorf("azure: owner must be non-empty")
	}
	p.mu.RLock()
	client, err := p.ownershipTagsClientLocked()
	p.mu.RUnlock()
	if err != nil {
		return err
	}
	scope, err := ownershipARMID(ref)
	if err != nil {
		return err
	}
	if err := client.UpdateAtScope(ctx, scope, armresources.TagsPatchResource{
		Operation: to.Ptr(armresources.TagsPatchOperationMerge),
		Properties: &armresources.Tags{
			Tags: map[string]*string{ownershipTagKey: to.Ptr(owner)},
		},
	}); err != nil {
		return fmt.Errorf("azure: tag %s/%s with owner %q: %w", ref.Type, ref.Name, owner, err)
	}
	return nil
}

func (p *AzureProvider) ListOwners(ctx context.Context, filter interfaces.OwnerFilter) ([]interfaces.ResourceOwner, error) {
	p.mu.RLock()
	resourceGroup := p.resourceGroup
	client, err := p.ownershipResourcesClientLocked()
	p.mu.RUnlock()
	if err != nil {
		return nil, err
	}

	tagFilter := fmt.Sprintf("tagName eq '%s'", azureODataLiteral(ownershipTagKey))
	if filter.Owner != "" {
		tagFilter += fmt.Sprintf(" and tagValue eq '%s'", azureODataLiteral(filter.Owner))
	}

	resources, err := client.ListByResourceGroup(ctx, resourceGroup, tagFilter)
	if err != nil {
		return nil, fmt.Errorf("azure: list owner tags: %w", err)
	}

	var owners []interfaces.ResourceOwner
	for _, resource := range resources {
		owner := ownerFromAzureTagMap(resource.Tags)
		if owner == "" && filter.Owner != "" {
			owner = filter.Owner
		}
		if owner == "" {
			continue
		}
		ref := refFromAzureResource(resource)
		if ref.ProviderID == "" {
			continue
		}
		if filter.ResourceType != "" && ref.Type != filter.ResourceType {
			continue
		}
		owners = append(owners, interfaces.ResourceOwner{Ref: ref, Owner: owner, Source: ownershipTagSource})
	}
	return owners, nil
}

func (p *AzureProvider) ownershipTagsClientLocked() (ownershipTagsClient, error) {
	if p.subscriptionID == "" {
		return nil, fmt.Errorf("azure: provider not initialized")
	}
	if p.ownershipTags == nil {
		return nil, fmt.Errorf("azure: ownership tags client not initialized")
	}
	return p.ownershipTags, nil
}

func (p *AzureProvider) ownershipResourcesClientLocked() (ownershipResourcesClient, error) {
	if p.subscriptionID == "" {
		return nil, fmt.Errorf("azure: provider not initialized")
	}
	if p.resourceGroup == "" {
		return nil, fmt.Errorf("azure: resource_group is required")
	}
	if p.ownershipResources == nil {
		return nil, fmt.Errorf("azure: ownership resources client not initialized")
	}
	return p.ownershipResources, nil
}

func ownershipARMID(ref interfaces.ResourceRef) (string, error) {
	if strings.HasPrefix(ref.ProviderID, "/subscriptions/") && strings.Contains(ref.ProviderID, "/providers/") {
		return ref.ProviderID, nil
	}
	return "", fmt.Errorf("%w for %s/%s: got %q", ErrOwnershipARMIDRequired, ref.Type, ref.Name, ref.ProviderID)
}

func ownerFromAzureTags(tags *armresources.Tags) string {
	if tags == nil {
		return ""
	}
	return ownerFromAzureTagMap(tags.Tags)
}

func ownerFromAzureTagMap(tags map[string]*string) string {
	if tags == nil || tags[ownershipTagKey] == nil {
		return ""
	}
	return *tags[ownershipTagKey]
}

func refFromAzureResource(resource armresources.GenericResourceExpanded) interfaces.ResourceRef {
	id := stringPtrValue(resource.ID)
	resourceType := workflowTypeFromAzureResourceType(stringPtrValue(resource.Type))
	if id == "" || resourceType == "" {
		return interfaces.ResourceRef{}
	}
	name := stringPtrValue(resource.Name)
	if name == "" {
		name = azureResourceNameFromID(id)
	}
	return interfaces.ResourceRef{
		Name:       name,
		Type:       resourceType,
		ProviderID: id,
	}
}

func workflowTypeFromAzureResourceType(resourceType string) string {
	switch strings.ToLower(resourceType) {
	case "microsoft.containerinstance/containergroups":
		return "infra.container_service"
	case "microsoft.containerservice/managedclusters":
		return "infra.k8s_cluster"
	case "microsoft.sql/servers", "microsoft.sql/servers/databases":
		return "infra.database"
	case "microsoft.cache/redis":
		return "infra.cache"
	case "microsoft.network/virtualnetworks":
		return "infra.vpc"
	case "microsoft.network/loadbalancers":
		return "infra.load_balancer"
	case "microsoft.network/dnszones":
		return "infra.dns"
	case "microsoft.containerregistry/registries":
		return "infra.registry"
	case "microsoft.apimanagement/service":
		return "infra.api_gateway"
	case "microsoft.network/networksecuritygroups":
		return "infra.firewall"
	case "microsoft.managedidentity/userassignedidentities":
		return "infra.iam_role"
	case "microsoft.storage/storageaccounts", "microsoft.storage/storageaccounts/blobservices/containers":
		return "infra.storage"
	case "microsoft.web/certificates":
		return "infra.certificate"
	default:
		return ""
	}
}

func azureResourceNameFromID(id string) string {
	id = strings.TrimRight(id, "/")
	if id == "" {
		return ""
	}
	parts := strings.Split(id, "/")
	return parts[len(parts)-1]
}

func azureODataLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

var _ interfaces.OwnershipProvider = (*AzureProvider)(nil)
