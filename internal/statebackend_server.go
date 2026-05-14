// Package internal — typed pb.IaCStateBackendServer implementation.
//
// Per decisions/0035 (one type carries both concerns), azureIaCServer ALSO
// serves the typed IaC state-backend contract: it persists IaC state via an
// AzureBlobIaCStateStore (ported from workflow core) and answers
// ListBackendNames with the single backend name "azure_blob".
//
// Hard invariants (strict-contracts force-cutover):
//   - NO structpb.Struct on the wire — the free-form Outputs / Config
//     map[string]any fields of IaCState cross as JSON bytes (outputs_json,
//     config_json), converted locally via encoding/json below.
//   - The store is lazily constructed: the host configures the backend (account
//     URL, container, credential) out-of-band; until then GetState/etc. return
//     a clear "not configured" error rather than panicking.
package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/GoCodeAlone/workflow-plugin-azure/internal/statebackend"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// azureStateBackendName is the single iac.state backend name this plugin serves.
const azureStateBackendName = "azure_blob"

// stateBackend holds the lazily-constructed azure_blob state store plus the
// guard that builds it once. It is embedded into azureIaCServer.
type stateBackend struct {
	mu    sync.Mutex
	store *statebackend.AzureBlobIaCStateStore
}

// resolveStore returns the configured store, or a clear error if the host has
// not yet provisioned an azure_blob backend for this plugin process.
//
// The state-backend contract has no Initialize RPC of its own — a future PR may
// add backend configuration plumbing. For now the store is set via
// setStateStore (used by tests / the engine wiring); an unset store yields a
// descriptive error rather than a nil-pointer panic.
func (b *stateBackend) resolveStore() (*statebackend.AzureBlobIaCStateStore, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.store == nil {
		return nil, fmt.Errorf("azure state backend: azure_blob backend is not configured")
	}
	return b.store, nil
}

// setStateStore injects the backing store. Used by the engine wiring and tests.
func (b *stateBackend) setStateStore(s *statebackend.AzureBlobIaCStateStore) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.store = s
}

// ── pb.IaCStateBackendServer methods (on azureIaCServer) ────────────────────

// GetState retrieves a state record by resource ID.
func (s *azureIaCServer) GetState(ctx context.Context, req *pb.GetStateRequest) (*pb.GetStateResponse, error) {
	store, err := s.stateBackend.resolveStore()
	if err != nil {
		return nil, err
	}
	st, err := store.GetState(ctx, req.GetResourceId())
	if err != nil {
		return nil, err
	}
	if st == nil {
		return &pb.GetStateResponse{Exists: false}, nil
	}
	pbState, err := iacStateToPB(st)
	if err != nil {
		return nil, fmt.Errorf("azure state backend: encode GetState response: %w", err)
	}
	return &pb.GetStateResponse{State: pbState, Exists: true}, nil
}

// SaveState inserts or replaces a state record.
func (s *azureIaCServer) SaveState(ctx context.Context, req *pb.SaveStateRequest) (*pb.SaveStateResponse, error) {
	store, err := s.stateBackend.resolveStore()
	if err != nil {
		return nil, err
	}
	st, err := iacStateFromPB(req.GetState())
	if err != nil {
		return nil, fmt.Errorf("azure state backend: decode SaveState request: %w", err)
	}
	if err := store.SaveState(ctx, st); err != nil {
		return nil, err
	}
	return &pb.SaveStateResponse{}, nil
}

// ListStates returns all state records matching the provided key=value filter.
func (s *azureIaCServer) ListStates(ctx context.Context, req *pb.ListStatesRequest) (*pb.ListStatesResponse, error) {
	store, err := s.stateBackend.resolveStore()
	if err != nil {
		return nil, err
	}
	states, err := store.ListStates(ctx, req.GetFilter())
	if err != nil {
		return nil, err
	}
	pbStates := make([]*pb.IaCState, 0, len(states))
	for _, st := range states {
		pbState, err := iacStateToPB(st)
		if err != nil {
			return nil, fmt.Errorf("azure state backend: encode ListStates response: %w", err)
		}
		pbStates = append(pbStates, pbState)
	}
	return &pb.ListStatesResponse{States: pbStates}, nil
}

// DeleteState removes a state record by resource ID.
func (s *azureIaCServer) DeleteState(ctx context.Context, req *pb.DeleteStateRequest) (*pb.DeleteStateResponse, error) {
	store, err := s.stateBackend.resolveStore()
	if err != nil {
		return nil, err
	}
	if err := store.DeleteState(ctx, req.GetResourceId()); err != nil {
		return nil, err
	}
	return &pb.DeleteStateResponse{}, nil
}

// Lock acquires an exclusive lock for the given resource ID.
func (s *azureIaCServer) Lock(ctx context.Context, req *pb.LockRequest) (*pb.LockResponse, error) {
	store, err := s.stateBackend.resolveStore()
	if err != nil {
		return nil, err
	}
	if err := store.Lock(ctx, req.GetResourceId()); err != nil {
		return nil, err
	}
	return &pb.LockResponse{}, nil
}

// Unlock releases the lock for the given resource ID.
func (s *azureIaCServer) Unlock(ctx context.Context, req *pb.UnlockRequest) (*pb.UnlockResponse, error) {
	store, err := s.stateBackend.resolveStore()
	if err != nil {
		return nil, err
	}
	if err := store.Unlock(ctx, req.GetResourceId()); err != nil {
		return nil, err
	}
	return &pb.UnlockResponse{}, nil
}

// ListBackendNames reports the iac.state backend names this plugin serves.
func (s *azureIaCServer) ListBackendNames(_ context.Context, _ *pb.ListBackendNamesRequest) (*pb.ListBackendNamesResponse, error) {
	return &pb.ListBackendNamesResponse{BackendNames: []string{azureStateBackendName}}, nil
}

// ── IaCState ⇄ pb.IaCState converters ───────────────────────────────────────
//
// Local re-implementation of workflow core's unexported iacStateToProto /
// iacStateFromProto. The Outputs / Config map[string]any fields cross the wire
// as JSON bytes (outputs_json / config_json) per the iac.proto invariant — NO
// structpb.

func iacStateToPB(st *statebackend.IaCState) (*pb.IaCState, error) {
	if st == nil {
		return nil, nil
	}
	outputsJSON, err := marshalIaCMap(st.Outputs)
	if err != nil {
		return nil, fmt.Errorf("marshal outputs: %w", err)
	}
	configJSON, err := marshalIaCMap(st.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	return &pb.IaCState{
		ResourceId:   st.ResourceID,
		ResourceType: st.ResourceType,
		Provider:     st.Provider,
		ProviderRef:  st.ProviderRef,
		ProviderId:   st.ProviderID,
		ConfigHash:   st.ConfigHash,
		Status:       st.Status,
		OutputsJson:  outputsJSON,
		ConfigJson:   configJSON,
		Dependencies: append([]string(nil), st.Dependencies...),
		CreatedAt:    st.CreatedAt,
		UpdatedAt:    st.UpdatedAt,
		Error:        st.Error,
	}, nil
}

func iacStateFromPB(s *pb.IaCState) (*statebackend.IaCState, error) {
	if s == nil {
		return nil, fmt.Errorf("iac state must not be nil")
	}
	outputs, err := unmarshalIaCMap(s.GetOutputsJson())
	if err != nil {
		return nil, fmt.Errorf("unmarshal outputs: %w", err)
	}
	config, err := unmarshalIaCMap(s.GetConfigJson())
	if err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &statebackend.IaCState{
		ResourceID:   s.GetResourceId(),
		ResourceType: s.GetResourceType(),
		Provider:     s.GetProvider(),
		ProviderRef:  s.GetProviderRef(),
		ProviderID:   s.GetProviderId(),
		ConfigHash:   s.GetConfigHash(),
		Status:       s.GetStatus(),
		Outputs:      outputs,
		Config:       config,
		Dependencies: append([]string(nil), s.GetDependencies()...),
		CreatedAt:    s.GetCreatedAt(),
		UpdatedAt:    s.GetUpdatedAt(),
		Error:        s.GetError(),
	}, nil
}

func marshalIaCMap(m map[string]any) ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func unmarshalIaCMap(b []byte) (map[string]any, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}
