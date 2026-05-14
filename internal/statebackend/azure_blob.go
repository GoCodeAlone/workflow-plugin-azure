// Package statebackend provides the azure_blob IaCStateBackend implementation,
// ported from workflow core's module/iac_state_azure.go (cloud-SDK-extraction
// effort, decisions/0033-0035). It is self-contained: it carries its own
// IaCState type, AzureBlobClient interface, and azureRealClient (azblob-backed)
// impl so the plugin can SERVE the azure_blob backend over the typed
// IaCStateBackend gRPC contract without depending on workflow core's unexported
// state helpers.
package statebackend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/lease"
)

// IaCState tracks the state of an infrastructure resource. Mirrors
// module.IaCState (workflow core module/iac_state.go) — kept local so this
// package is self-contained.
type IaCState struct {
	ResourceID   string         `json:"resource_id"`
	ResourceType string         `json:"resource_type"` // e.g. "kubernetes", "ecs"
	Provider     string         `json:"provider"`      // e.g. "aws", "gcp", "local"
	ProviderRef  string         `json:"provider_ref,omitempty"`
	ProviderID   string         `json:"provider_id,omitempty"`
	ConfigHash   string         `json:"config_hash,omitempty"`
	Status       string         `json:"status"`  // planned, provisioning, active, destroying, destroyed, error
	Outputs      map[string]any `json:"outputs"` // provider-specific outputs
	Config       map[string]any `json:"config"`  // the config used to provision
	Dependencies []string       `json:"dependencies,omitempty"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
	Error        string         `json:"error,omitempty"`
}

// sanitizeID replaces path-unsafe characters so resource IDs can be used as blob names.
func sanitizeID(id string) string {
	id = strings.ReplaceAll(id, "/", "_")
	id = strings.ReplaceAll(id, "\\", "_")
	return id
}

// matchesFilter returns true if state satisfies all entries in the filter map.
func matchesFilter(st *IaCState, filter map[string]string) bool {
	for k, v := range filter {
		switch k {
		case "resource_type":
			if st.ResourceType != v {
				return false
			}
		case "provider":
			if st.Provider != v {
				return false
			}
		case "status":
			if st.Status != v {
				return false
			}
		}
	}
	return true
}

// ErrAzureBlobNotFound is returned when a blob does not exist.
var ErrAzureBlobNotFound = errors.New("azure blob: not found")

// AzureBlobClient abstracts Azure Blob Storage operations used by AzureBlobIaCStateStore.
type AzureBlobClient interface {
	DownloadBlob(ctx context.Context, name string) ([]byte, error)
	UploadBlob(ctx context.Context, name string, data []byte, contentType string) error
	DeleteBlob(ctx context.Context, name string) error
	ListBlobs(ctx context.Context, prefix string) ([]string, error)
	AcquireLease(ctx context.Context, name string, durationSeconds int32) (leaseID string, err error)
	ReleaseLease(ctx context.Context, name, leaseID string) error
}

// AzureBlobIaCStateStore persists IaC state as JSON blobs in Azure Blob Storage.
// Locking uses Azure blob leases for atomic advisory locking.
type AzureBlobIaCStateStore struct {
	client    AzureBlobClient
	container string
	prefix    string
	mu        sync.Mutex
	leaseIDs  map[string]string // resourceID -> leaseID
}

// NewAzureBlobIaCStateStore creates an Azure Blob-backed state store.
// accountURL should be of the form https://<account>.blob.core.windows.net.
func NewAzureBlobIaCStateStore(accountURL, container, prefix string, cred azblob.SharedKeyCredential) (*AzureBlobIaCStateStore, error) {
	if container == "" {
		return nil, fmt.Errorf("iac azure state: container must not be empty")
	}
	if prefix == "" {
		prefix = "iac-state/"
	}
	client, err := azblob.NewClientWithSharedKeyCredential(accountURL, &cred, nil)
	if err != nil {
		return nil, fmt.Errorf("iac azure state: create client: %w", err)
	}
	return &AzureBlobIaCStateStore{
		client:    &azureRealClient{client: client, container: container},
		container: container,
		prefix:    prefix,
		leaseIDs:  make(map[string]string),
	}, nil
}

// NewAzureBlobIaCStateStoreWithClient creates a store with an injected client (for testing).
func NewAzureBlobIaCStateStoreWithClient(client AzureBlobClient, container, prefix string) *AzureBlobIaCStateStore {
	if prefix == "" {
		prefix = "iac-state/"
	}
	return &AzureBlobIaCStateStore{
		client:    client,
		container: container,
		prefix:    prefix,
		leaseIDs:  make(map[string]string),
	}
}

func (s *AzureBlobIaCStateStore) blobName(resourceID string) string {
	return s.prefix + sanitizeID(resourceID) + ".json"
}

func (s *AzureBlobIaCStateStore) lockBlobName(resourceID string) string {
	return s.prefix + sanitizeID(resourceID) + ".lock"
}

// GetState retrieves a state record by resource ID. Returns nil, nil when not found.
func (s *AzureBlobIaCStateStore) GetState(ctx context.Context, resourceID string) (*IaCState, error) {
	data, err := s.client.DownloadBlob(ctx, s.blobName(resourceID))
	if err != nil {
		if errors.Is(err, ErrAzureBlobNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("iac azure state: GetState %q: %w", resourceID, err)
	}
	var st IaCState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("iac azure state: GetState %q: unmarshal: %w", resourceID, err)
	}
	return &st, nil
}

// SaveState writes the state record as a JSON blob.
func (s *AzureBlobIaCStateStore) SaveState(ctx context.Context, state *IaCState) error {
	if state == nil {
		return fmt.Errorf("iac azure state: SaveState: state must not be nil")
	}
	if state.ResourceID == "" {
		return fmt.Errorf("iac azure state: SaveState: resource_id must not be empty")
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("iac azure state: SaveState %q: marshal: %w", state.ResourceID, err)
	}
	if err := s.client.UploadBlob(ctx, s.blobName(state.ResourceID), data, "application/json"); err != nil {
		return fmt.Errorf("iac azure state: SaveState %q: upload: %w", state.ResourceID, err)
	}
	return nil
}

// ListStates lists all state blobs and returns those matching the filter.
func (s *AzureBlobIaCStateStore) ListStates(ctx context.Context, filter map[string]string) ([]*IaCState, error) {
	names, err := s.client.ListBlobs(ctx, s.prefix)
	if err != nil {
		return nil, fmt.Errorf("iac azure state: ListStates: %w", err)
	}
	var results []*IaCState
	for _, name := range names {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := s.client.DownloadBlob(ctx, name)
		if err != nil {
			// A canceled / deadlined context must abort the listing rather
			// than silently return partial results; only genuinely unreadable
			// blobs are skipped.
			if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, fmt.Errorf("iac azure state: ListStates: %w", err)
			}
			continue
		}
		var st IaCState
		if err := json.Unmarshal(data, &st); err != nil {
			continue
		}
		if matchesFilter(&st, filter) {
			results = append(results, &st)
		}
	}
	return results, nil
}

// DeleteState removes the state blob for resourceID.
func (s *AzureBlobIaCStateStore) DeleteState(ctx context.Context, resourceID string) error {
	if err := s.client.DeleteBlob(ctx, s.blobName(resourceID)); err != nil {
		if errors.Is(err, ErrAzureBlobNotFound) {
			return fmt.Errorf("iac azure state: DeleteState %q: not found", resourceID)
		}
		return fmt.Errorf("iac azure state: DeleteState %q: %w", resourceID, err)
	}
	return nil
}

// Lock acquires a blob lease on the lock blob for resourceID (60-second duration).
func (s *AzureBlobIaCStateStore) Lock(ctx context.Context, resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	lockBlob := s.lockBlobName(resourceID)
	leaseID, err := s.client.AcquireLease(ctx, lockBlob, 60)
	if err != nil {
		if strings.Contains(err.Error(), "already leased") || strings.Contains(err.Error(), "leased") {
			return fmt.Errorf("iac azure state: Lock %q: resource is already locked", resourceID)
		}
		return fmt.Errorf("iac azure state: Lock %q: %w", resourceID, err)
	}
	s.leaseIDs[resourceID] = leaseID
	return nil
}

// Unlock releases the lease on the lock blob for resourceID.
func (s *AzureBlobIaCStateStore) Unlock(ctx context.Context, resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	leaseID, held := s.leaseIDs[resourceID]
	if !held {
		return fmt.Errorf("iac azure state: Unlock %q: not locked", resourceID)
	}
	lockBlob := s.lockBlobName(resourceID)
	if err := s.client.ReleaseLease(ctx, lockBlob, leaseID); err != nil {
		return fmt.Errorf("iac azure state: Unlock %q: %w", resourceID, err)
	}
	delete(s.leaseIDs, resourceID)
	return nil
}

// azureRealClient wraps the actual Azure SDK client.
type azureRealClient struct {
	client    *azblob.Client
	container string
}

func (c *azureRealClient) DownloadBlob(ctx context.Context, name string) ([]byte, error) {
	resp, err := c.client.DownloadStream(ctx, c.container, name, nil)
	if err != nil {
		if isAzureNotFound(err) {
			return nil, ErrAzureBlobNotFound
		}
		return nil, err
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *azureRealClient) UploadBlob(ctx context.Context, name string, data []byte, contentType string) error {
	_, err := c.client.UploadBuffer(ctx, c.container, name, data, &azblob.UploadBufferOptions{
		HTTPHeaders: &blob.HTTPHeaders{BlobContentType: &contentType},
	})
	return err
}

func (c *azureRealClient) DeleteBlob(ctx context.Context, name string) error {
	_, err := c.client.DeleteBlob(ctx, c.container, name, nil)
	if err != nil && isAzureNotFound(err) {
		return ErrAzureBlobNotFound
	}
	return err
}

func (c *azureRealClient) ListBlobs(ctx context.Context, prefix string) ([]string, error) {
	pager := c.client.NewListBlobsFlatPager(c.container, &azblob.ListBlobsFlatOptions{
		Prefix: &prefix,
	})
	var names []string
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Segment.BlobItems {
			if item.Name != nil {
				names = append(names, *item.Name)
			}
		}
	}
	return names, nil
}

func (c *azureRealClient) AcquireLease(ctx context.Context, name string, durationSeconds int32) (string, error) {
	blobClient := c.client.ServiceClient().NewContainerClient(c.container).NewBlobClient(name)
	leaseClient, err := lease.NewBlobClient(blobClient, nil)
	if err != nil {
		return "", err
	}
	resp, err := leaseClient.AcquireLease(ctx, durationSeconds, nil)
	if err != nil {
		return "", err
	}
	if resp.LeaseID == nil {
		return "", fmt.Errorf("no lease ID returned")
	}
	return *resp.LeaseID, nil
}

func (c *azureRealClient) ReleaseLease(ctx context.Context, name, leaseID string) error {
	blobClient := c.client.ServiceClient().NewContainerClient(c.container).NewBlobClient(name)
	leaseClient, err := lease.NewBlobClient(blobClient, &lease.BlobClientOptions{LeaseID: &leaseID})
	if err != nil {
		return err
	}
	_, err = leaseClient.ReleaseLease(ctx, nil)
	return err
}

func isAzureNotFound(err error) bool {
	var respErr *azcore.ResponseError
	return errors.As(err, &respErr) && respErr.StatusCode == 404
}
