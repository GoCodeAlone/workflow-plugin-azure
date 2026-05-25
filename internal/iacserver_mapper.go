package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	azureCollectorModuleName = "observability-collector"
	azureCollectorImage      = "otel/opentelemetry-collector-contrib:latest"
	azureCollectorType       = "infra.container_service"
)

// MapRequirements maps canonical derived-IaC requirements into Azure-owned
// resource shapes. The v1 mapper emits a generic collector container service.
// If an application needs a true Azure Container Apps sidecar, it can supply an
// explicit module with the same satisfies keys.
func (s *azureIaCServer) MapRequirements(_ context.Context, req *pb.MapRequirementsRequest) (*pb.MapRequirementsResponse, error) {
	if req.GetProvider() != "" && req.GetProvider() != "azure" {
		return nil, status.Errorf(codes.InvalidArgument, "azure mapper cannot satisfy provider %q", req.GetProvider())
	}

	resp := &pb.MapRequirementsResponse{}
	var accepted []*pb.IaCRequirement
	for _, requirement := range req.GetRequirements() {
		switch diag := azureRejectUnsupportedRequirement(req.GetRuntime(), requirement); {
		case diag != nil:
			resp.Rejected = append(resp.Rejected, diag)
		default:
			accepted = append(accepted, requirement)
			resp.AcceptedKeys = append(resp.AcceptedKeys, requirement.GetKey())
		}
	}
	if len(accepted) == 0 {
		return resp, nil
	}

	configJSON, err := json.Marshal(azureCollectorModuleConfig(accepted))
	if err != nil {
		return nil, fmt.Errorf("azure requirement mapper: encode collector config: %w", err)
	}
	resp.Modules = append(resp.Modules, &pb.DerivedModuleSpec{
		Name:       azureCollectorModuleName,
		Type:       azureCollectorType,
		Satisfies:  append([]string(nil), resp.GetAcceptedKeys()...),
		ConfigJson: configJSON,
	})
	resp.Notes = append(resp.Notes, &pb.RequirementNote{
		Key:         strings.Join(resp.GetAcceptedKeys(), ","),
		Message:     "Azure derivation emits a generic OTel Collector container service. Use an explicit infra.container_service module with the same satisfies keys when an application needs a provider-specific sidecar shape.",
		Interactive: false,
	})
	return resp, nil
}

func azureRejectUnsupportedRequirement(runtime pb.RequirementRuntime, req *pb.IaCRequirement) *pb.RequirementDiagnostic {
	key := req.GetKey()
	if req.GetKind() != pb.RequirementKind_REQUIREMENT_KIND_OBSERVABILITY {
		return azureRequirementDiagnostic(key, "unsupported_kind", "azure can only derive observability requirements today")
	}
	if hint := req.GetResourceTypeHint(); hint != "" && hint != azureCollectorType {
		return azureRequirementDiagnostic(key, "unsupported_resource_type_hint",
			fmt.Sprintf("azure observability derivation emits %s, not %s", azureCollectorType, hint))
	}
	if runtime != pb.RequirementRuntime_REQUIREMENT_RUNTIME_AZURE_CONTAINER_APPS {
		return azureRequirementDiagnostic(key, "unsupported_runtime", "azure observability derivation currently targets Azure Container Apps intent")
	}
	if !azureRequirementAllowsRuntime(req, runtime) {
		return azureRequirementDiagnostic(key, "unsupported_runtime", "requirement does not allow Azure Container Apps")
	}
	if !azureRequirementAllowsDeploymentMode(req) {
		return azureRequirementDiagnostic(key, "unsupported_deployment_mode",
			"azure maps sidecar intent to an explicit or sibling collector service; daemonset mode belongs to AKS and is not emitted by this mapper yet")
	}
	return nil
}

func azureRequirementAllowsRuntime(req *pb.IaCRequirement, runtime pb.RequirementRuntime) bool {
	if len(req.GetRuntimes()) == 0 {
		return true
	}
	for _, candidate := range req.GetRuntimes() {
		if candidate == runtime {
			return true
		}
	}
	return false
}

func azureRequirementAllowsDeploymentMode(req *pb.IaCRequirement) bool {
	modes := req.GetDeploymentModes()
	if len(modes) == 0 {
		return true
	}
	for _, mode := range modes {
		switch mode {
		case pb.DeploymentMode_DEPLOYMENT_MODE_SIDECAR,
			pb.DeploymentMode_DEPLOYMENT_MODE_SIBLING_SERVICE,
			pb.DeploymentMode_DEPLOYMENT_MODE_MANAGED:
			return true
		}
	}
	return false
}

func azureRequirementDiagnostic(key, code, message string) *pb.RequirementDiagnostic {
	return &pb.RequirementDiagnostic{Key: key, Code: code, Message: message}
}

func azureCollectorModuleConfig(reqs []*pb.IaCRequirement) map[string]any {
	signals := azureRequestedSignals(reqs)
	backends := azureRequestedBackends(reqs)
	collectorConfig := azureBuildCollectorConfig(signals, backends)

	envVars := map[string]any{
		"OTELCOL_CONFIG": collectorConfig,
	}
	secretVars := make(map[string]any)
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_OTEL) {
		envVars["OTEL_EXPORTER_OTLP_ENDPOINT"] = "${vars.otel_exporter_otlp_endpoint}"
	}
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_DATADOG) {
		envVars["DD_SITE"] = "${vars.datadog_site}"
		secretVars["DD_API_KEY"] = "${secrets.datadog_api_key}"
	}
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_LOKI) {
		envVars["LOKI_ENDPOINT"] = "${vars.loki_endpoint}"
	}
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_GRAFANA) {
		envVars["GRAFANA_OTLP_ENDPOINT"] = "${vars.grafana_otlp_endpoint}"
		secretVars["GRAFANA_OTLP_HEADERS"] = "${secrets.grafana_otlp_headers}"
	}

	return map[string]any{
		"image":           azureCollectorImage,
		"command":         []any{"otelcol-contrib", "--config=env:OTELCOL_CONFIG"},
		"replicas":        1,
		"ports":           azureCollectorPorts(backends),
		"env_vars":        envVars,
		"env_vars_secret": secretVars,
	}
}

func azureRequestedSignals(reqs []*pb.IaCRequirement) map[pb.TelemetrySignal]bool {
	out := make(map[pb.TelemetrySignal]bool)
	for _, req := range reqs {
		for _, signal := range req.GetTelemetrySignals() {
			if signal != pb.TelemetrySignal_TELEMETRY_SIGNAL_UNSPECIFIED {
				out[signal] = true
			}
		}
	}
	if len(out) == 0 {
		out[pb.TelemetrySignal_TELEMETRY_SIGNAL_TRACES] = true
		out[pb.TelemetrySignal_TELEMETRY_SIGNAL_METRICS] = true
		out[pb.TelemetrySignal_TELEMETRY_SIGNAL_LOGS] = true
	}
	return out
}

func azureRequestedBackends(reqs []*pb.IaCRequirement) map[pb.ObservabilityBackend]bool {
	out := make(map[pb.ObservabilityBackend]bool)
	for _, req := range reqs {
		for _, backend := range req.GetObservabilityBackends() {
			if backend != pb.ObservabilityBackend_OBSERVABILITY_BACKEND_UNSPECIFIED {
				out[backend] = true
			}
		}
	}
	if len(out) == 0 {
		out[pb.ObservabilityBackend_OBSERVABILITY_BACKEND_OTEL] = true
	}
	return out
}

func azureCollectorPorts(backends map[pb.ObservabilityBackend]bool) []any {
	ports := []any{
		map[string]any{"port": 4317, "public": false},
		map[string]any{"port": 4318, "public": false},
	}
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_PROMETHEUS) {
		ports = append(ports, map[string]any{"port": 9464, "public": false})
	}
	return ports
}

func azureBuildCollectorConfig(signals map[pb.TelemetrySignal]bool, backends map[pb.ObservabilityBackend]bool) string {
	var b strings.Builder
	b.WriteString("receivers:\n")
	b.WriteString("  otlp:\n")
	b.WriteString("    protocols:\n")
	b.WriteString("      grpc:\n")
	b.WriteString("        endpoint: 0.0.0.0:4317\n")
	b.WriteString("      http:\n")
	b.WriteString("        endpoint: 0.0.0.0:4318\n")
	b.WriteString("processors:\n")
	b.WriteString("  batch: {}\n")
	b.WriteString("exporters:\n")
	azureWriteExporters(&b, backends)
	b.WriteString("service:\n")
	b.WriteString("  pipelines:\n")
	if signals[pb.TelemetrySignal_TELEMETRY_SIGNAL_TRACES] {
		azureWritePipeline(&b, "traces", azureExportersForSignal(pb.TelemetrySignal_TELEMETRY_SIGNAL_TRACES, backends))
	}
	if signals[pb.TelemetrySignal_TELEMETRY_SIGNAL_METRICS] {
		azureWritePipeline(&b, "metrics", azureExportersForSignal(pb.TelemetrySignal_TELEMETRY_SIGNAL_METRICS, backends))
	}
	if signals[pb.TelemetrySignal_TELEMETRY_SIGNAL_LOGS] {
		azureWritePipeline(&b, "logs", azureExportersForSignal(pb.TelemetrySignal_TELEMETRY_SIGNAL_LOGS, backends))
	}
	return b.String()
}

func azureWriteExporters(b *strings.Builder, backends map[pb.ObservabilityBackend]bool) {
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_OTEL) {
		b.WriteString("  otlp:\n")
		b.WriteString("    endpoint: ${env:OTEL_EXPORTER_OTLP_ENDPOINT}\n")
	}
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_DATADOG) {
		b.WriteString("  datadog:\n")
		b.WriteString("    api:\n")
		b.WriteString("      key: ${env:DD_API_KEY}\n")
		b.WriteString("      site: ${env:DD_SITE}\n")
	}
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_PROMETHEUS) {
		b.WriteString("  prometheus:\n")
		b.WriteString("    endpoint: 0.0.0.0:9464\n")
	}
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_LOKI) {
		b.WriteString("  loki:\n")
		b.WriteString("    endpoint: ${env:LOKI_ENDPOINT}\n")
	}
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_GRAFANA) {
		b.WriteString("  otlp/grafana_otlp:\n")
		b.WriteString("    endpoint: ${env:GRAFANA_OTLP_ENDPOINT}\n")
		b.WriteString("    headers:\n")
		b.WriteString("      authorization: ${env:GRAFANA_OTLP_HEADERS}\n")
	}
}

func azureWritePipeline(b *strings.Builder, name string, exporters []string) {
	if len(exporters) == 0 {
		return
	}
	b.WriteString("    ")
	b.WriteString(name)
	b.WriteString(":\n")
	b.WriteString("      receivers: [otlp]\n")
	b.WriteString("      processors: [batch]\n")
	b.WriteString("      exporters: [")
	b.WriteString(strings.Join(exporters, ", "))
	b.WriteString("]\n")
}

func azureExportersForSignal(signal pb.TelemetrySignal, backends map[pb.ObservabilityBackend]bool) []string {
	var exporters []string
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_OTEL) {
		exporters = append(exporters, "otlp")
	}
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_DATADOG) {
		exporters = append(exporters, "datadog")
	}
	if signal == pb.TelemetrySignal_TELEMETRY_SIGNAL_METRICS &&
		azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_PROMETHEUS) {
		exporters = append(exporters, "prometheus")
	}
	if signal == pb.TelemetrySignal_TELEMETRY_SIGNAL_LOGS &&
		azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_LOKI) {
		exporters = append(exporters, "loki")
	}
	if azureHasBackend(backends, pb.ObservabilityBackend_OBSERVABILITY_BACKEND_GRAFANA) {
		exporters = append(exporters, "otlp/grafana_otlp")
	}
	sort.Strings(exporters)
	return exporters
}

func azureHasBackend(backends map[pb.ObservabilityBackend]bool, backend pb.ObservabilityBackend) bool {
	return backends[backend]
}
