package internal

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerinstance/armcontainerinstance/v2"
	"github.com/GoCodeAlone/workflow/interfaces"
)

type azureRunnerClient interface {
	CreateOrUpdate(ctx context.Context, resourceGroup, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error)
	Get(ctx context.Context, resourceGroup, name string) (armcontainerinstance.ContainerGroup, error)
	ListLogs(ctx context.Context, resourceGroup, containerGroup, container string, tail int32) (string, error)
}

type realAzureRunnerClient struct {
	groups     *armcontainerinstance.ContainerGroupsClient
	containers *armcontainerinstance.ContainersClient
}

func (c *realAzureRunnerClient) CreateOrUpdate(ctx context.Context, rg, name string, cg armcontainerinstance.ContainerGroup) (armcontainerinstance.ContainerGroup, error) {
	poller, err := c.groups.BeginCreateOrUpdate(ctx, rg, name, cg, nil)
	if err != nil {
		return armcontainerinstance.ContainerGroup{}, err
	}
	res, err := poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: 5 * time.Second})
	if err != nil {
		return armcontainerinstance.ContainerGroup{}, err
	}
	return res.ContainerGroup, nil
}

func (c *realAzureRunnerClient) Get(ctx context.Context, rg, name string) (armcontainerinstance.ContainerGroup, error) {
	res, err := c.groups.Get(ctx, rg, name, nil)
	if err != nil {
		return armcontainerinstance.ContainerGroup{}, err
	}
	return res.ContainerGroup, nil
}

func (c *realAzureRunnerClient) ListLogs(ctx context.Context, rg, group, container string, tail int32) (string, error) {
	res, err := c.containers.ListLogs(ctx, rg, group, container, &armcontainerinstance.ContainersClientListLogsOptions{Tail: &tail})
	if err != nil {
		return "", err
	}
	if res.Content == nil {
		return "", nil
	}
	return *res.Content, nil
}

var _ interfaces.IaCProviderRunner = (*AzureProvider)(nil)

func (p *AzureProvider) RunJob(ctx context.Context, spec interfaces.JobSpec) (*interfaces.JobHandle, error) {
	p.mu.RLock()
	client := p.runnerClient
	rg := p.resourceGroup
	location := p.location
	p.mu.RUnlock()
	if client == nil || rg == "" || location == "" {
		return nil, fmt.Errorf("azure runner: provider is not initialized")
	}
	if strings.TrimSpace(spec.Image) == "" {
		return nil, fmt.Errorf("azure runner: image is required")
	}
	if strings.TrimSpace(spec.RunCommand) == "" {
		return nil, fmt.Errorf("azure runner: run_command is required")
	}

	name := azureJobName(spec.Name)
	cg := armcontainerinstance.ContainerGroup{
		Location: azureString(location),
		Properties: &armcontainerinstance.ContainerGroupPropertiesProperties{
			Containers: []*armcontainerinstance.Container{{
				Name: azureString(name),
				Properties: &armcontainerinstance.ContainerProperties{
					Image:                azureString(spec.Image),
					Command:              []*string{azureString("/bin/sh"), azureString("-c"), azureString(spec.RunCommand)},
					EnvironmentVariables: azureJobEnvironment(spec),
					Resources: &armcontainerinstance.ResourceRequirements{
						Requests: &armcontainerinstance.ResourceRequests{
							CPU:        azurePtr(float64(1)),
							MemoryInGB: azurePtr(float64(1.5)),
						},
					},
				},
			}},
			OSType:        azurePtr(armcontainerinstance.OperatingSystemTypesLinux),
			RestartPolicy: azurePtr(armcontainerinstance.ContainerGroupRestartPolicyNever),
		},
	}

	created, err := client.CreateOrUpdate(ctx, rg, name, cg)
	if err != nil {
		return nil, fmt.Errorf("azure runner: create container group %q: %w", name, err)
	}
	id := azureStringVal(created.ID)
	if id == "" {
		id = name
	}
	return &interfaces.JobHandle{
		ID:       id,
		Name:     name,
		Provider: "azure",
		Metadata: map[string]string{
			"resource_group":  rg,
			"container_group": name,
			"container":       name,
			"location":        location,
		},
	}, nil
}

func (p *AzureProvider) JobStatus(ctx context.Context, handle interfaces.JobHandle) (*interfaces.JobStatusReply, error) {
	p.mu.RLock()
	client := p.runnerClient
	defaultRG := p.resourceGroup
	p.mu.RUnlock()
	if client == nil {
		return nil, fmt.Errorf("azure runner: provider is not initialized")
	}
	rg := handle.Metadata["resource_group"]
	if rg == "" {
		rg = defaultRG
	}
	group := handle.Metadata["container_group"]
	if group == "" {
		group = handle.Name
	}
	if group == "" {
		return nil, fmt.Errorf("azure runner: container_group metadata is required")
	}
	cg, err := client.Get(ctx, rg, group)
	if err != nil {
		return nil, fmt.Errorf("azure runner: get container group %q: %w", group, err)
	}
	state, exitCode, message := azureJobState(cg)
	return &interfaces.JobStatusReply{Handle: handle, State: state, ExitCode: exitCode, Message: message}, nil
}

func (p *AzureProvider) JobLogs(ctx context.Context, handle interfaces.JobHandle, sink interfaces.LogCaptureSink) error {
	p.mu.RLock()
	client := p.runnerClient
	defaultRG := p.resourceGroup
	p.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("azure runner: provider is not initialized")
	}
	rg := handle.Metadata["resource_group"]
	if rg == "" {
		rg = defaultRG
	}
	group := handle.Metadata["container_group"]
	container := handle.Metadata["container"]
	if group == "" {
		group = handle.Name
	}
	if container == "" {
		container = group
	}
	if group == "" || container == "" {
		return fmt.Errorf("azure runner: container_group and container metadata are required")
	}
	content, err := client.ListLogs(ctx, rg, group, container, 200)
	if err != nil {
		return fmt.Errorf("azure runner: list logs for %q: %w", group, err)
	}
	if sink == nil {
		return nil
	}
	if content != "" {
		if err := sink.WriteLogChunk(interfaces.LogChunk{Data: []byte(content), Source: "stdout"}); err != nil {
			return err
		}
	}
	return sink.WriteLogChunk(interfaces.LogChunk{EOF: true})
}

func azureJobEnvironment(spec interfaces.JobSpec) []*armcontainerinstance.EnvironmentVariable {
	var out []*armcontainerinstance.EnvironmentVariable
	for _, key := range sortedStringMapKeys(spec.EnvVars) {
		out = append(out, &armcontainerinstance.EnvironmentVariable{Name: azureString(key), Value: azureString(spec.EnvVars[key])})
	}
	for _, key := range sortedStringMapKeys(spec.EnvVarsSecret) {
		out = append(out, &armcontainerinstance.EnvironmentVariable{Name: azureString(key), SecureValue: azureString(spec.EnvVarsSecret[key])})
	}
	return out
}

func sortedStringMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var nonAzureJobName = regexp.MustCompile(`[^a-z0-9-]+`)

func azureJobName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = nonAzureJobName.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "provider-ephemeral-job"
	}
	suffix := fmt.Sprintf("-%d", time.Now().UnixNano())
	maxBase := 63 - len(suffix)
	if len(name) > maxBase {
		name = strings.TrimRight(name[:maxBase], "-")
	}
	return name + suffix
}

func azureJobState(cg armcontainerinstance.ContainerGroup) (interfaces.JobState, int, string) {
	if cg.Properties == nil || len(cg.Properties.Containers) == 0 || cg.Properties.Containers[0].Properties == nil ||
		cg.Properties.Containers[0].Properties.InstanceView == nil ||
		cg.Properties.Containers[0].Properties.InstanceView.CurrentState == nil {
		return interfaces.JobStatePending, 0, azureProvisioningState(cg)
	}
	current := cg.Properties.Containers[0].Properties.InstanceView.CurrentState
	state := strings.ToLower(azureStringVal(current.State))
	exit := 0
	if current.ExitCode != nil {
		exit = int(*current.ExitCode)
	}
	msg := azureStringVal(current.DetailStatus)
	switch state {
	case "running":
		return interfaces.JobStateRunning, exit, msg
	case "terminated":
		if exit == 0 {
			return interfaces.JobStateSucceeded, exit, msg
		}
		return interfaces.JobStateFailed, exit, msg
	case "waiting":
		return interfaces.JobStatePending, exit, msg
	default:
		return interfaces.JobStateUnknown, exit, msg
	}
}

func azureProvisioningState(cg armcontainerinstance.ContainerGroup) string {
	if cg.Properties == nil {
		return ""
	}
	return azureStringVal(cg.Properties.ProvisioningState)
}

func azureString(s string) *string { return &s }

func azureStringVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func azurePtr[T any](v T) *T { return &v }
