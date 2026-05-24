package driver

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// DNSClient is the narrow interface for Azure DNS operations.
type DNSClient interface {
	CreateOrUpdate(ctx context.Context, resourceGroup, zoneName string, zone armdns.Zone) (armdns.Zone, error)
	Get(ctx context.Context, resourceGroup, zoneName string) (armdns.Zone, error)
	Delete(ctx context.Context, resourceGroup, zoneName string) error
	ListRecordSets(ctx context.Context, resourceGroup, zoneName string) ([]*armdns.RecordSet, error)
	CreateOrUpdateRecordSet(ctx context.Context, resourceGroup, zoneName, recordName string, recordType armdns.RecordType, record armdns.RecordSet) (armdns.RecordSet, error)
}

type realDNSClient struct {
	zones   *armdns.ZonesClient
	records *armdns.RecordSetsClient
}

func (c *realDNSClient) CreateOrUpdate(ctx context.Context, rg, zoneName string, zone armdns.Zone) (armdns.Zone, error) {
	res, err := c.zones.CreateOrUpdate(ctx, rg, zoneName, zone, nil)
	if err != nil {
		return armdns.Zone{}, err
	}
	return res.Zone, nil
}

func (c *realDNSClient) Get(ctx context.Context, rg, zoneName string) (armdns.Zone, error) {
	res, err := c.zones.Get(ctx, rg, zoneName, nil)
	if err != nil {
		return armdns.Zone{}, err
	}
	return res.Zone, nil
}

func (c *realDNSClient) Delete(ctx context.Context, rg, zoneName string) error {
	poller, err := c.zones.BeginDelete(ctx, rg, zoneName, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, &runtime.PollUntilDoneOptions{Frequency: pollFrequency})
	return err
}

func (c *realDNSClient) ListRecordSets(ctx context.Context, rg, zoneName string) ([]*armdns.RecordSet, error) {
	pager := c.records.NewListByDNSZonePager(rg, zoneName, nil)
	var out []*armdns.RecordSet
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Value...)
	}
	return out, nil
}

func (c *realDNSClient) CreateOrUpdateRecordSet(ctx context.Context, rg, zoneName, recordName string, recordType armdns.RecordType, record armdns.RecordSet) (armdns.RecordSet, error) {
	res, err := c.records.CreateOrUpdate(ctx, rg, zoneName, recordName, recordType, record, nil)
	if err != nil {
		return armdns.RecordSet{}, err
	}
	return res.RecordSet, nil
}

// DNSDriver manages Azure DNS zones (infra.dns).
type DNSDriver struct {
	resourceGroup string
	location      string
	client        DNSClient
}

var _ interfaces.ResourceDriver = (*DNSDriver)(nil)

// SensitiveKeys returns output keys whose values should be masked in logs and plan output.
func (d *DNSDriver) SensitiveKeys() []string { return nil }

func NewDNSDriver(resourceGroup, location string, client DNSClient) *DNSDriver {
	return &DNSDriver{resourceGroup: resourceGroup, location: location, client: client}
}

func (d *DNSDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	zoneName := configStr(spec.Config, "zone_name", spec.Name)
	zoneType := configStr(spec.Config, "zone_type", "Public")

	zone := armdns.Zone{
		Location: str("global"),
		Properties: &armdns.ZoneProperties{
			ZoneType: ptrOf(armdns.ZoneType(zoneType)),
		},
	}

	result, err := d.client.CreateOrUpdate(ctx, d.resourceGroup, zoneName, zone)
	if err != nil {
		return nil, fmt.Errorf("dns: create %q: %w", zoneName, err)
	}
	if err := d.upsertConfiguredRecords(ctx, zoneName, spec.Config); err != nil {
		return nil, err
	}
	return dnsToOutput(spec.Name, result), nil
}

func (d *DNSDriver) Read(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	zoneName := ref.Name
	result, err := d.client.Get(ctx, d.resourceGroup, zoneName)
	if err != nil {
		return nil, fmt.Errorf("dns: get %q: %w", zoneName, err)
	}
	out := dnsToOutput(ref.Name, result)
	records, err := d.client.ListRecordSets(ctx, d.resourceGroup, zoneName)
	if err != nil {
		return nil, fmt.Errorf("dns: list records %q: %w", zoneName, err)
	}
	normalizedRecords := azureRecordSetOutputs(records)
	out.Outputs["records"] = normalizedRecords
	out.Outputs["record_count"] = len(normalizedRecords)
	return out, nil
}

func (d *DNSDriver) Update(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return d.Create(ctx, spec)
}

func (d *DNSDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.client.Delete(ctx, d.resourceGroup, ref.Name)
}

func (d *DNSDriver) Diff(_ context.Context, desired interfaces.ResourceSpec, current *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	if current == nil {
		return &interfaces.DiffResult{NeedsUpdate: true}, nil
	}
	var changes []interfaces.FieldChange
	if zoneType, ok := desired.Config["zone_type"].(string); ok {
		if cur, ok := current.Outputs["zone_type"].(string); ok && zoneType != cur {
			changes = append(changes, interfaces.FieldChange{Path: "zone_type", Old: cur, New: zoneType, ForceNew: true})
		}
	}
	return &interfaces.DiffResult{NeedsUpdate: len(changes) > 0, NeedsReplace: len(changes) > 0, Changes: changes}, nil
}

func (d *DNSDriver) HealthCheck(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	out, err := d.Read(ctx, ref)
	if err != nil {
		return &interfaces.HealthResult{Healthy: false, Message: err.Error()}, nil
	}
	return &interfaces.HealthResult{Healthy: out.Status != "unknown", Message: out.Status}, nil
}

func (d *DNSDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, fmt.Errorf("dns: scale not supported")
}

func (d *DNSDriver) upsertConfiguredRecords(ctx context.Context, zoneName string, config map[string]any) error {
	records, err := azureConfiguredRecordSets(config)
	if err != nil {
		return err
	}
	for _, record := range records {
		if _, err := d.client.CreateOrUpdateRecordSet(ctx, d.resourceGroup, zoneName, record.name, record.recordType, record.recordSet); err != nil {
			return fmt.Errorf("dns: upsert record %s/%s %s: %w", zoneName, record.name, record.recordType, err)
		}
	}
	return nil
}

func dnsToOutput(name string, z armdns.Zone) *interfaces.ResourceOutput {
	outputs := map[string]any{}
	if z.Name != nil {
		outputs["domain"] = *z.Name
		outputs["zone_name"] = *z.Name
	}
	if z.Properties != nil {
		if z.Properties.NumberOfRecordSets != nil {
			outputs["record_sets"] = *z.Properties.NumberOfRecordSets
			outputs["record_count"] = *z.Properties.NumberOfRecordSets
		}
		if z.Properties.ZoneType != nil {
			outputs["zone_type"] = string(*z.Properties.ZoneType)
		}
		if len(z.Properties.NameServers) > 0 {
			outputs["name_servers"] = azureNameServers(z.Properties.NameServers)
		}
	}
	outputs["authority"] = map[string]any{
		"role":         "target_authoritative_dns",
		"dns_host":     "Azure DNS",
		"name_servers": azureOutputNameServers(outputs),
	}
	status := "active"
	if z.ID == nil {
		status = "unknown"
	}
	return &interfaces.ResourceOutput{
		Name:       name,
		Type:       "infra.dns",
		ProviderID: strVal(z.ID),
		Outputs:    outputs,
		Status:     status,
	}
}

func azureNameServers(values []*string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != nil {
			out = append(out, *value)
		}
	}
	return out
}

func azureOutputNameServers(outputs map[string]any) []string {
	values, _ := outputs["name_servers"].([]string)
	return append([]string(nil), values...)
}

type azureConfiguredRecordSet struct {
	name       string
	recordType armdns.RecordType
	recordSet  armdns.RecordSet
}

func azureConfiguredRecordSets(config map[string]any) ([]azureConfiguredRecordSet, error) {
	rawRecords, ok := config["records"]
	if !ok || rawRecords == nil {
		return nil, nil
	}
	items, ok := rawRecords.([]any)
	if !ok {
		return nil, fmt.Errorf("dns: records must be a list")
	}
	out := make([]azureConfiguredRecordSet, 0, len(items))
	for i, item := range items {
		recordConfig, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("dns: records[%d] must be an object", i)
		}
		recordType := armdns.RecordType(strings.ToUpper(configStr(recordConfig, "type", "")))
		if recordType == "" {
			return nil, fmt.Errorf("dns: records[%d].type is required", i)
		}
		name := configStr(recordConfig, "name", "@")
		ttl := int64(configInt(recordConfig, "ttl", 3600))
		props := &armdns.RecordSetProperties{TTL: &ttl}
		if err := fillAzureRecordValues(props, recordType, recordConfig); err != nil {
			return nil, fmt.Errorf("dns: records[%d]: %w", i, err)
		}
		out = append(out, azureConfiguredRecordSet{
			name:       name,
			recordType: recordType,
			recordSet:  armdns.RecordSet{Properties: props},
		})
	}
	return out, nil
}

func fillAzureRecordValues(props *armdns.RecordSetProperties, recordType armdns.RecordType, config map[string]any) error {
	values := configList(config, "values")
	if len(values) == 0 {
		if value, ok := config["value"]; ok {
			values = []any{value}
		}
	}
	if len(values) == 0 {
		return fmt.Errorf("%s record requires at least one value", recordType)
	}
	switch recordType {
	case armdns.RecordTypeA:
		for _, value := range values {
			props.ARecords = append(props.ARecords, &armdns.ARecord{IPv4Address: str(fmt.Sprint(value))})
		}
	case armdns.RecordTypeAAAA:
		for _, value := range values {
			props.AaaaRecords = append(props.AaaaRecords, &armdns.AaaaRecord{IPv6Address: str(fmt.Sprint(value))})
		}
	case armdns.RecordTypeCNAME:
		if len(values) != 1 {
			return fmt.Errorf("CNAME requires exactly one value")
		}
		props.CnameRecord = &armdns.CnameRecord{Cname: str(fmt.Sprint(values[0]))}
	case armdns.RecordTypeMX:
		for _, value := range values {
			m, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("MX values must be objects")
			}
			pref := int32(configInt(m, "preference", 0))
			props.MxRecords = append(props.MxRecords, &armdns.MxRecord{Preference: &pref, Exchange: str(configStr(m, "exchange", ""))})
		}
	case armdns.RecordTypeNS:
		for _, value := range values {
			props.NsRecords = append(props.NsRecords, &armdns.NsRecord{Nsdname: str(fmt.Sprint(value))})
		}
	case armdns.RecordTypeTXT:
		for _, value := range values {
			props.TxtRecords = append(props.TxtRecords, &armdns.TxtRecord{Value: []*string{str(fmt.Sprint(value))}})
		}
	default:
		return fmt.Errorf("unsupported record type %q", recordType)
	}
	return nil
}

func azureRecordSetOutputs(records []*armdns.RecordSet) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		normalized := azureRecordSetOutput(record)
		if normalized != nil {
			out = append(out, normalized)
		}
	}
	return out
}

func azureRecordSetOutput(record *armdns.RecordSet) map[string]any {
	recordType := azureRecordType(record)
	if recordType == "" || record.Properties == nil {
		return nil
	}
	out := map[string]any{
		"name": strVal(record.Name),
		"type": recordType,
	}
	if record.Properties.TTL != nil {
		out["ttl"] = *record.Properties.TTL
	}
	switch armdns.RecordType(recordType) {
	case armdns.RecordTypeA:
		values := make([]string, 0, len(record.Properties.ARecords))
		for _, value := range record.Properties.ARecords {
			if value != nil && value.IPv4Address != nil {
				values = append(values, *value.IPv4Address)
			}
		}
		out["values"] = values
	case armdns.RecordTypeAAAA:
		values := make([]string, 0, len(record.Properties.AaaaRecords))
		for _, value := range record.Properties.AaaaRecords {
			if value != nil && value.IPv6Address != nil {
				values = append(values, *value.IPv6Address)
			}
		}
		out["values"] = values
	case armdns.RecordTypeCNAME:
		if record.Properties.CnameRecord != nil && record.Properties.CnameRecord.Cname != nil {
			out["values"] = []string{*record.Properties.CnameRecord.Cname}
		}
	case armdns.RecordTypeMX:
		values := make([]map[string]any, 0, len(record.Properties.MxRecords))
		for _, value := range record.Properties.MxRecords {
			if value == nil {
				continue
			}
			values = append(values, map[string]any{
				"preference": int32Val(value.Preference),
				"exchange":   strVal(value.Exchange),
			})
		}
		out["values"] = values
	case armdns.RecordTypeNS:
		values := make([]string, 0, len(record.Properties.NsRecords))
		for _, value := range record.Properties.NsRecords {
			if value != nil && value.Nsdname != nil {
				values = append(values, *value.Nsdname)
			}
		}
		out["values"] = values
	case armdns.RecordTypeTXT:
		values := make([]string, 0, len(record.Properties.TxtRecords))
		for _, value := range record.Properties.TxtRecords {
			if value == nil {
				continue
			}
			parts := make([]string, 0, len(value.Value))
			for _, part := range value.Value {
				if part != nil {
					parts = append(parts, *part)
				}
			}
			values = append(values, strings.Join(parts, ""))
		}
		out["values"] = values
	default:
		return nil
	}
	return out
}

func azureRecordType(record *armdns.RecordSet) string {
	if record.Type == nil {
		return ""
	}
	value := *record.Type
	if idx := strings.LastIndex(value, "/"); idx >= 0 {
		value = value[idx+1:]
	}
	return strings.ToUpper(value)
}

func configList(config map[string]any, key string) []any {
	switch values := config[key].(type) {
	case []any:
		return values
	case []string:
		out := make([]any, 0, len(values))
		for _, value := range values {
			out = append(out, value)
		}
		return out
	default:
		return nil
	}
}

func int32Val(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}
