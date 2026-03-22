package driver

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/redis/armredis"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type mockRedisClient struct {
	createFn func(ctx context.Context, rg, name string, params armredis.CreateParameters) (armredis.ResourceInfo, error)
	getFn    func(ctx context.Context, rg, name string) (armredis.ResourceInfo, error)
	deleteFn func(ctx context.Context, rg, name string) error
}

func (m *mockRedisClient) Create(ctx context.Context, rg, name string, params armredis.CreateParameters) (armredis.ResourceInfo, error) {
	return m.createFn(ctx, rg, name, params)
}

func (m *mockRedisClient) Get(ctx context.Context, rg, name string) (armredis.ResourceInfo, error) {
	return m.getFn(ctx, rg, name)
}

func (m *mockRedisClient) Delete(ctx context.Context, rg, name string) error {
	return m.deleteFn(ctx, rg, name)
}

func TestRedisDriver_Create(t *testing.T) {
	prov := armredis.ProvisioningStateSucceeded
	host := "test-redis.redis.cache.windows.net"
	port := int32(6379)
	sslPort := int32(6380)

	client := &mockRedisClient{
		createFn: func(_ context.Context, _, name string, _ armredis.CreateParameters) (armredis.ResourceInfo, error) {
			return armredis.ResourceInfo{
				ID: str("/subscriptions/sub/rg/redis/" + name),
				Properties: &armredis.Properties{
					ProvisioningState: &prov,
					HostName:         &host,
					Port:             &port,
					SSLPort:          &sslPort,
				},
			}, nil
		},
	}

	drv := NewRedisDriver("rg", "eastus", client)
	out, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-redis",
		Type:   "infra.cache",
		Config: map[string]any{"capacity": 1},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
	if out.Outputs["host"] != host {
		t.Errorf("host = %v, want %s", out.Outputs["host"], host)
	}
}

func TestRedisDriver_Create_Error(t *testing.T) {
	client := &mockRedisClient{
		createFn: func(_ context.Context, _, _ string, _ armredis.CreateParameters) (armredis.ResourceInfo, error) {
			return armredis.ResourceInfo{}, errors.New("quota exceeded")
		},
	}

	drv := NewRedisDriver("rg", "eastus", client)
	_, err := drv.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "test-redis",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRedisDriver_Read(t *testing.T) {
	prov := armredis.ProvisioningStateSucceeded
	host := "test.redis.cache.windows.net"
	port := int32(6379)
	sslPort := int32(6380)

	client := &mockRedisClient{
		getFn: func(_ context.Context, _, name string) (armredis.ResourceInfo, error) {
			return armredis.ResourceInfo{
				ID: str("/sub/rg/" + name),
				Properties: &armredis.Properties{
					ProvisioningState: &prov,
					HostName:         &host,
					Port:             &port,
					SSLPort:          &sslPort,
				},
			}, nil
		},
	}

	drv := NewRedisDriver("rg", "eastus", client)
	out, err := drv.Read(context.Background(), interfaces.ResourceRef{Name: "test-redis", Type: "infra.cache"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Status != "Succeeded" {
		t.Errorf("status = %q, want Succeeded", out.Status)
	}
	if out.Outputs["host"] != host {
		t.Errorf("host = %v, want %s", out.Outputs["host"], host)
	}
}

func TestRedisDriver_Update(t *testing.T) {
	prov := armredis.ProvisioningStateSucceeded
	called := false

	client := &mockRedisClient{
		createFn: func(_ context.Context, _, name string, _ armredis.CreateParameters) (armredis.ResourceInfo, error) {
			called = true
			return armredis.ResourceInfo{
				ID:         str("/sub/rg/" + name),
				Properties: &armredis.Properties{ProvisioningState: &prov},
			}, nil
		},
	}

	drv := NewRedisDriver("rg", "eastus", client)
	out, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-redis"}, interfaces.ResourceSpec{
		Name:   "test-redis",
		Config: map[string]any{"capacity": 2},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !called {
		t.Error("expected Create to be called")
	}
	_ = out
}

func TestRedisDriver_Update_Error(t *testing.T) {
	client := &mockRedisClient{
		createFn: func(_ context.Context, _, _ string, _ armredis.CreateParameters) (armredis.ResourceInfo, error) {
			return armredis.ResourceInfo{}, errors.New("update failed")
		},
	}

	drv := NewRedisDriver("rg", "eastus", client)
	_, err := drv.Update(context.Background(), interfaces.ResourceRef{Name: "test-redis"}, interfaces.ResourceSpec{
		Name:   "test-redis",
		Config: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRedisDriver_Delete(t *testing.T) {
	deleted := false
	client := &mockRedisClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			deleted = true
			return nil
		},
	}

	drv := NewRedisDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-redis"})
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("expected Delete to be called")
	}
}

func TestRedisDriver_Delete_Error(t *testing.T) {
	client := &mockRedisClient{
		deleteFn: func(_ context.Context, _, _ string) error {
			return errors.New("not found")
		},
	}

	drv := NewRedisDriver("rg", "eastus", client)
	err := drv.Delete(context.Background(), interfaces.ResourceRef{Name: "test-redis"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRedisDriver_Diff_HasChanges(t *testing.T) {
	drv := NewRedisDriver("rg", "eastus", nil)
	current := &interfaces.ResourceOutput{
		Outputs: map[string]any{"capacity": 1},
	}
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{"capacity": 2},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when capacity changes")
	}
}

func TestRedisDriver_Diff_NoChanges(t *testing.T) {
	drv := NewRedisDriver("rg", "eastus", nil)
	// capacity is int in both sides — NeedsUpdate should be false when same
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{
		Config: map[string]any{},
	}, &interfaces.ResourceOutput{Outputs: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=false when no config changes")
	}
}

func TestRedisDriver_Diff_NilCurrent(t *testing.T) {
	drv := NewRedisDriver("rg", "eastus", nil)
	diff, err := drv.Diff(context.Background(), interfaces.ResourceSpec{Name: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.NeedsUpdate {
		t.Error("expected NeedsUpdate=true when current is nil")
	}
}

func TestRedisDriver_HealthCheck(t *testing.T) {
	prov := armredis.ProvisioningStateSucceeded
	host := "test.redis.cache.windows.net"
	port := int32(6379)
	sslPort := int32(6380)

	client := &mockRedisClient{
		getFn: func(_ context.Context, _, name string) (armredis.ResourceInfo, error) {
			return armredis.ResourceInfo{
				ID: str("/sub/rg/" + name),
				Properties: &armredis.Properties{
					ProvisioningState: &prov,
					HostName:         &host,
					Port:             &port,
					SSLPort:          &sslPort,
				},
			}, nil
		},
	}

	drv := NewRedisDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-redis"})
	if err != nil {
		t.Fatal(err)
	}
	if !h.Healthy {
		t.Errorf("expected healthy, got %q", h.Message)
	}
}

func TestRedisDriver_HealthCheck_Unhealthy(t *testing.T) {
	client := &mockRedisClient{
		getFn: func(_ context.Context, _, _ string) (armredis.ResourceInfo, error) {
			return armredis.ResourceInfo{}, errors.New("cache not found")
		},
	}

	drv := NewRedisDriver("rg", "eastus", client)
	h, err := drv.HealthCheck(context.Background(), interfaces.ResourceRef{Name: "test-redis"})
	if err != nil {
		t.Fatal(err)
	}
	if h.Healthy {
		t.Error("expected unhealthy when get fails")
	}
}
