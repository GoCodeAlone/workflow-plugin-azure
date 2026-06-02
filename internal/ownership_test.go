package internal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type fakeTokenCredential struct{}

func (fakeTokenCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "test", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

type fakeOwnershipTagsClient struct {
	getScopes    []string
	getResponses []armresources.TagsResource
	updateScopes []string
	updateParams []armresources.TagsPatchResource
}

func (f *fakeOwnershipTagsClient) GetAtScope(ctx context.Context, scope string) (armresources.TagsResource, error) {
	f.getScopes = append(f.getScopes, scope)
	if len(f.getResponses) == 0 {
		return armresources.TagsResource{}, nil
	}
	out := f.getResponses[0]
	f.getResponses = f.getResponses[1:]
	return out, nil
}

func (f *fakeOwnershipTagsClient) UpdateAtScope(ctx context.Context, scope string, parameters armresources.TagsPatchResource) error {
	f.updateScopes = append(f.updateScopes, scope)
	f.updateParams = append(f.updateParams, parameters)
	return nil
}

type fakeOwnershipResourcesClient struct {
	resourceGroup string
	filter        string
	resources     []armresources.GenericResourceExpanded
}

func (f *fakeOwnershipResourcesClient) ListByResourceGroup(ctx context.Context, resourceGroup, filter string) ([]armresources.GenericResourceExpanded, error) {
	f.resourceGroup = resourceGroup
	f.filter = filter
	return f.resources, nil
}

func TestOwnershipProviderCompileGuard(t *testing.T) {
	var _ interfaces.OwnershipProvider = (*AzureProvider)(nil)
}

func TestSetOwnerMergesWorkflowOwnerTagAtARMScope(t *testing.T) {
	tags := &fakeOwnershipTagsClient{}
	p := initializedOwnershipProvider(tags, &fakeOwnershipResourcesClient{})
	id := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/api"

	if err := p.SetOwner(context.Background(), interfaces.ResourceRef{Name: "api", Type: "infra.container_service", ProviderID: id}, "workflow"); err != nil {
		t.Fatalf("SetOwner: %v", err)
	}

	if len(tags.updateScopes) != 1 || tags.updateScopes[0] != id {
		t.Fatalf("update scopes = %v, want [%s]", tags.updateScopes, id)
	}
	param := tags.updateParams[0]
	if param.Operation == nil || *param.Operation != armresources.TagsPatchOperationMerge {
		t.Fatalf("operation = %v, want Merge", param.Operation)
	}
	if param.Properties == nil || param.Properties.Tags[ownershipTagKey] == nil || *param.Properties.Tags[ownershipTagKey] != "workflow" {
		t.Fatalf("tags = %#v, want %q=workflow", param.Properties, ownershipTagKey)
	}
}

func TestSetOwnerRejectsNonARMProviderID(t *testing.T) {
	p := initializedOwnershipProvider(&fakeOwnershipTagsClient{}, &fakeOwnershipResourcesClient{})

	err := p.SetOwner(context.Background(), interfaces.ResourceRef{Name: "container", Type: "infra.storage", ProviderID: "container-name"}, "workflow")
	if err == nil {
		t.Fatal("SetOwner returned nil, want ARM ID error")
	}
	if !errors.Is(err, ErrOwnershipARMIDRequired) {
		t.Fatalf("SetOwner error = %v, want ErrOwnershipARMIDRequired", err)
	}
}

func TestGetOwnerReadsWorkflowOwnerTag(t *testing.T) {
	id := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Sql/servers/sql1/databases/app"
	tags := &fakeOwnershipTagsClient{
		getResponses: []armresources.TagsResource{
			{Properties: &armresources.Tags{Tags: map[string]*string{
				"env":           to.Ptr("prod"),
				ownershipTagKey: to.Ptr("data"),
			}}},
		},
	}
	p := initializedOwnershipProvider(tags, &fakeOwnershipResourcesClient{})

	owner, err := p.GetOwner(context.Background(), interfaces.ResourceRef{Name: "app", Type: "infra.database", ProviderID: id})
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	if owner.Owner != "data" {
		t.Fatalf("Owner = %q, want data", owner.Owner)
	}
	if owner.Source != ownershipTagSource {
		t.Fatalf("Source = %q, want %q", owner.Source, ownershipTagSource)
	}
	if owner.Ref.ProviderID != id {
		t.Fatalf("Ref.ProviderID = %q, want %q", owner.Ref.ProviderID, id)
	}
}

func TestListOwnersFiltersByOwnerAndResourceType(t *testing.T) {
	aciID := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/api"
	sqlID := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Sql/servers/sql1/databases/app"
	resources := &fakeOwnershipResourcesClient{
		resources: []armresources.GenericResourceExpanded{
			{
				ID:   to.Ptr(aciID),
				Name: to.Ptr("api"),
				Type: to.Ptr("Microsoft.ContainerInstance/containerGroups"),
				Tags: map[string]*string{ownershipTagKey: to.Ptr("workflow")},
			},
			{
				ID:   to.Ptr(sqlID),
				Name: to.Ptr("app"),
				Type: to.Ptr("Microsoft.Sql/servers/databases"),
				Tags: map[string]*string{ownershipTagKey: to.Ptr("workflow")},
			},
		},
	}
	p := initializedOwnershipProvider(&fakeOwnershipTagsClient{}, resources)

	owners, err := p.ListOwners(context.Background(), interfaces.OwnerFilter{Owner: "workflow", ResourceType: "infra.container_service"})
	if err != nil {
		t.Fatalf("ListOwners: %v", err)
	}
	if resources.resourceGroup != "rg" {
		t.Fatalf("resourceGroup = %q, want rg", resources.resourceGroup)
	}
	if resources.filter != "tagName eq 'workflow-owner' and tagValue eq 'workflow'" {
		t.Fatalf("filter = %q", resources.filter)
	}
	if len(owners) != 1 {
		t.Fatalf("owners len = %d, want 1: %#v", len(owners), owners)
	}
	got := owners[0]
	if got.Owner != "workflow" || got.Source != ownershipTagSource {
		t.Fatalf("owner metadata = %#v, want owner workflow source %q", got, ownershipTagSource)
	}
	if got.Ref.Name != "api" || got.Ref.Type != "infra.container_service" || got.Ref.ProviderID != aciID {
		t.Fatalf("ref = %#v, want api infra.container_service %s", got.Ref, aciID)
	}
}

func initializedOwnershipProvider(tags ownershipTagsClient, resources ownershipResourcesClient) *AzureProvider {
	return &AzureProvider{
		subscriptionID:     "sub",
		resourceGroup:      "rg",
		credential:         &fakeTokenCredential{},
		drivers:            make(map[string]interfaces.ResourceDriver),
		ownershipTags:      tags,
		ownershipResources: resources,
	}
}
