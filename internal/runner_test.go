package internal

import (
	"context"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerinstance/armcontainerinstance/v2"
	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/grpc"
)

type fakeAzureRunnerClient struct {
	createdName string
	createdRG   string
	createdCG   armcontainerinstance.ContainerGroup
	gotGroup    armcontainerinstance.ContainerGroup
	logs        string
}

func (f *fakeAzureRunnerClient) CreateOrUpdate(_ context.Context, rg, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
	f.createdRG = rg
	f.createdName = name
	f.createdCG = cg
	cg.ID = azureString("/subscriptions/sub/resourceGroups/" + rg + "/providers/Microsoft.ContainerInstance/containerGroups/" + name)
	return cg, nil
}

func (f *fakeAzureRunnerClient) Get(_ context.Context, _, _ string) (armcontainerinstance.ContainerGroup, error) {
	return f.gotGroup, nil
}

func (f *fakeAzureRunnerClient) ListLogs(_ context.Context, _, _, _ string, _ int32) (string, error) {
	return f.logs, nil
}

func TestAzureRunner_RunJobCreatesNeverRestartContainerGroup(t *testing.T) {
	client := &fakeAzureRunnerClient{}
	p := &AzureProvider{
		resourceGroup: "rg",
		location:      "eastus",
		runnerClient:  client,
	}

	handle, err := p.RunJob(context.Background(), interfaces.JobSpec{
		Name:          "Migrate DB!",
		Image:         "example.azurecr.io/app:migrate",
		RunCommand:    "bin/migrate up",
		EnvVars:       map[string]string{"PLAIN": "value"},
		EnvVarsSecret: map[string]string{"DATABASE_URL": "secret://database/url"},
	})
	if err != nil {
		t.Fatalf("RunJob returned error: %v", err)
	}
	if handle.Provider != "azure" || handle.Metadata["container_group"] != "migrate-db" {
		t.Fatalf("handle = %+v", handle)
	}
	if client.createdRG != "rg" || client.createdName != "migrate-db" {
		t.Fatalf("created %s/%s, want rg/migrate-db", client.createdRG, client.createdName)
	}
	props := client.createdCG.Properties
	if props == nil || props.RestartPolicy == nil || *props.RestartPolicy != armcontainerinstance.ContainerGroupRestartPolicyNever {
		t.Fatalf("restart policy = %#v, want Never", props)
	}
	container := props.Containers[0]
	if got := azureStringVal(container.Properties.Image); got != "example.azurecr.io/app:migrate" {
		t.Fatalf("image = %q", got)
	}
	cmd := container.Properties.Command
	if len(cmd) != 3 || azureStringVal(cmd[0]) != "/bin/sh" || azureStringVal(cmd[1]) != "-c" || azureStringVal(cmd[2]) != "bin/migrate up" {
		t.Fatalf("command = %#v", cmd)
	}
	if !hasAzureEnv(container.Properties.EnvironmentVariables, "PLAIN", "value", false) {
		t.Fatalf("missing plain env: %#v", container.Properties.EnvironmentVariables)
	}
	if !hasAzureEnv(container.Properties.EnvironmentVariables, "DATABASE_URL", "secret://database/url", true) {
		t.Fatalf("missing secure env: %#v", container.Properties.EnvironmentVariables)
	}
}

func TestAzureRunner_StatusAndLogs(t *testing.T) {
	exit := int32(7)
	state := "Terminated"
	detail := "failed"
	client := &fakeAzureRunnerClient{
		logs: "migration failed\n",
		gotGroup: armcontainerinstance.ContainerGroup{
			Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{
				Containers: []*armcontainerinstance.Container{{
					Properties: &armcontainerinstance.ContainerProperties{
						InstanceView: &armcontainerinstance.ContainerPropertiesInstanceView{
							CurrentState: &armcontainerinstance.ContainerState{
								State:        &state,
								ExitCode:     &exit,
								DetailStatus: &detail,
							},
						},
					},
				}},
			},
		},
	}
	p := &AzureProvider{resourceGroup: "rg", runnerClient: client}
	handle := interfaces.JobHandle{Name: "job", Metadata: map[string]string{"container_group": "job", "container": "job"}}

	status, err := p.JobStatus(context.Background(), handle)
	if err != nil {
		t.Fatalf("JobStatus returned error: %v", err)
	}
	if status.State != interfaces.JobStateFailed || status.ExitCode != 7 || status.Message != "failed" {
		t.Fatalf("status = %+v", status)
	}

	sink := &runnerSink{}
	if err := p.JobLogs(context.Background(), handle, sink); err != nil {
		t.Fatalf("JobLogs returned error: %v", err)
	}
	if string(sink.data) != "migration failed\n" || !sink.eof {
		t.Fatalf("sink data=%q eof=%v", string(sink.data), sink.eof)
	}
}

func TestAzureRunnerServerAutoRegisters(t *testing.T) {
	server := grpc.NewServer()
	if err := sdk.RegisterAllIaCProviderServices(server, newAzureIaCServer(New(Version))); err != nil {
		t.Fatalf("RegisterAllIaCProviderServices: %v", err)
	}
	if _, ok := server.GetServiceInfo()[pb.IaCProviderRunner_ServiceDesc.ServiceName]; !ok {
		t.Fatalf("IaCProviderRunner service was not registered")
	}
}

func hasAzureEnv(values []*armcontainerinstance.EnvironmentVariable, key, value string, secure bool) bool {
	for _, env := range values {
		if azureStringVal(env.Name) != key {
			continue
		}
		if secure {
			return azureStringVal(env.SecureValue) == value && env.Value == nil
		}
		return azureStringVal(env.Value) == value && env.SecureValue == nil
	}
	return false
}

type runnerSink struct {
	data []byte
	eof  bool
}

func (s *runnerSink) WriteLogChunk(chunk interfaces.LogChunk) error {
	if chunk.EOF {
		s.eof = true
		return nil
	}
	if strings.Contains(strings.ToLower(chunk.Source), "stderr") {
		return nil
	}
	s.data = append(s.data, chunk.Data...)
	return nil
}
