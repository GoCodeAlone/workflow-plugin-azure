package internal

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow-plugin-azure/internal/statebackend"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// Compile-time guard: azureIaCServer MUST satisfy the typed state-backend
// contract so the SDK serve hook auto-registers it at plugin startup.
var _ pb.IaCStateBackendServer = (*azureIaCServer)(nil)

func TestIaCServer_ListBackendNames(t *testing.T) {
	s := NewIaCServer()
	resp, err := s.ListBackendNames(context.Background(), &pb.ListBackendNamesRequest{})
	if err != nil {
		t.Fatalf("ListBackendNames: %v", err)
	}
	got := resp.GetBackendNames()
	if len(got) != 1 || got[0] != "azure_blob" {
		t.Errorf("ListBackendNames = %v, want [azure_blob]", got)
	}
}

func TestIaCServer_StateBackend_NotConfigured(t *testing.T) {
	s := NewIaCServer()
	// With no store injected, the state RPCs must return a clear error rather
	// than panicking on a nil store.
	if _, err := s.GetState(context.Background(), &pb.GetStateRequest{ResourceId: "x"}); err == nil {
		t.Error("GetState: expected error when backend not configured")
	}
	if _, err := s.SaveState(context.Background(), &pb.SaveStateRequest{State: &pb.IaCState{ResourceId: "x"}}); err == nil {
		t.Error("SaveState: expected error when backend not configured")
	}
}

func TestIaCServer_StateBackend_RoundTrip(t *testing.T) {
	s := NewIaCServer()
	store := statebackend.NewAzureBlobIaCStateStoreWithClient(newMockAzureClient(), "test-container", "iac-state/")
	s.stateBackend.setStateStore(store)

	ctx := context.Background()
	in := &pb.IaCState{
		ResourceId:   "az-rt",
		ResourceType: "kubernetes",
		Provider:     "azure",
		Status:       "active",
		OutputsJson:  []byte(`{"fqdn":"myapp.azurewebsites.net"}`),
		ConfigJson:   []byte(`{"region":"eastus"}`),
	}
	if _, err := s.SaveState(ctx, &pb.SaveStateRequest{State: in}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	got, err := s.GetState(ctx, &pb.GetStateRequest{ResourceId: "az-rt"})
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if !got.GetExists() {
		t.Fatal("GetState: expected exists=true")
	}
	if got.GetState().GetProvider() != "azure" {
		t.Errorf("Provider = %q, want azure", got.GetState().GetProvider())
	}
	if string(got.GetState().GetOutputsJson()) != `{"fqdn":"myapp.azurewebsites.net"}` {
		t.Errorf("OutputsJson round-trip mismatch: %s", got.GetState().GetOutputsJson())
	}

	list, err := s.ListStates(ctx, &pb.ListStatesRequest{})
	if err != nil {
		t.Fatalf("ListStates: %v", err)
	}
	if len(list.GetStates()) != 1 {
		t.Errorf("ListStates = %d, want 1", len(list.GetStates()))
	}

	if _, err := s.Lock(ctx, &pb.LockRequest{ResourceId: "az-rt"}); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if _, err := s.Unlock(ctx, &pb.UnlockRequest{ResourceId: "az-rt"}); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	if _, err := s.DeleteState(ctx, &pb.DeleteStateRequest{ResourceId: "az-rt"}); err != nil {
		t.Fatalf("DeleteState: %v", err)
	}
	after, err := s.GetState(ctx, &pb.GetStateRequest{ResourceId: "az-rt"})
	if err != nil {
		t.Fatalf("GetState after delete: %v", err)
	}
	if after.GetExists() {
		t.Error("GetState after delete: expected exists=false")
	}
}

// newMockAzureClient is an in-memory AzureBlobClient for the round-trip test.
type mockStateBackendClient struct {
	blobs  map[string][]byte
	leases map[string]string
}

func newMockAzureClient() *mockStateBackendClient {
	return &mockStateBackendClient{
		blobs:  make(map[string][]byte),
		leases: make(map[string]string),
	}
}

func (m *mockStateBackendClient) DownloadBlob(_ context.Context, name string) ([]byte, error) {
	data, ok := m.blobs[name]
	if !ok {
		return nil, statebackend.ErrAzureBlobNotFound
	}
	return data, nil
}

func (m *mockStateBackendClient) UploadBlob(_ context.Context, name string, data []byte, _ string) error {
	m.blobs[name] = data
	return nil
}

func (m *mockStateBackendClient) DeleteBlob(_ context.Context, name string) error {
	if _, ok := m.blobs[name]; !ok {
		return statebackend.ErrAzureBlobNotFound
	}
	delete(m.blobs, name)
	return nil
}

func (m *mockStateBackendClient) ListBlobs(_ context.Context, prefix string) ([]string, error) {
	var names []string
	for name := range m.blobs {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			names = append(names, name)
		}
	}
	return names, nil
}

func (m *mockStateBackendClient) AcquireLease(_ context.Context, name string, _ int32) (string, error) {
	if _, ok := m.blobs[name]; !ok {
		m.blobs[name] = []byte{}
	}
	if m.leases[name] != "" {
		return "", &leasedError{name: name}
	}
	id := "lease-" + name
	m.leases[name] = id
	return id, nil
}

func (m *mockStateBackendClient) ReleaseLease(_ context.Context, name, leaseID string) error {
	if m.leases[name] != leaseID {
		return &leasedError{name: name}
	}
	delete(m.leases, name)
	delete(m.blobs, name)
	return nil
}

type leasedError struct{ name string }

func (e *leasedError) Error() string { return "blob " + e.name + " is already leased" }
