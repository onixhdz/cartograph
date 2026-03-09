package datasource

import (
	"context"
	"testing"
	"time"
)

// mockDataSource is a test implementation of DataSource.
type mockDataSource struct {
	info          DataSourceInfo
	resourceTypes []ResourceType
	configErr     error
	ingestErr     error
	ingestFunc    func(ctx context.Context, builder GraphBuilder, opts IngestOptions) error
}

func (m *mockDataSource) Info() DataSourceInfo { return m.info }

func (m *mockDataSource) Configure(_ map[string]any) error { return m.configErr }

func (m *mockDataSource) Ingest(ctx context.Context, builder GraphBuilder, opts IngestOptions) error {
	if m.ingestFunc != nil {
		return m.ingestFunc(ctx, builder, opts)
	}
	return m.ingestErr
}

func (m *mockDataSource) ResourceTypes() []ResourceType { return m.resourceTypes }

// TestDataSourceInterfaceCompliance verifies the mock satisfies DataSource.
func TestDataSourceInterfaceCompliance(t *testing.T) {
	var ds DataSource = &mockDataSource{
		info: DataSourceInfo{
			Name:        "test",
			Description: "test data source",
			Version:     "1.0.0",
		},
		resourceTypes: []ResourceType{
			{Name: "test_vm", Kind: "VirtualMachine", Description: "A VM"},
		},
	}

	info := ds.Info()
	if info.Name != "test" {
		t.Errorf("Name = %q, want test", info.Name)
	}
	if info.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", info.Version)
	}

	if err := ds.Configure(map[string]any{"key": "val"}); err != nil {
		t.Fatal(err)
	}

	types := ds.ResourceTypes()
	if len(types) != 1 {
		t.Fatalf("got %d resource types, want 1", len(types))
	}
	if types[0].Name != "test_vm" {
		t.Errorf("ResourceType.Name = %q, want test_vm", types[0].Name)
	}
	if types[0].Kind != "VirtualMachine" {
		t.Errorf("ResourceType.Kind = %q, want VirtualMachine", types[0].Kind)
	}
}

// TestIngestOptions verifies default values and field access.
func TestIngestOptions(t *testing.T) {
	opts := IngestOptions{
		ResourceTypes: []string{"vm", "bucket"},
		CacheTTL:      5 * time.Minute,
		Concurrency:   10,
		Quals:         map[string]any{"region": "us-east-1"},
	}

	if len(opts.ResourceTypes) != 2 {
		t.Errorf("ResourceTypes len = %d, want 2", len(opts.ResourceTypes))
	}
	if opts.CacheTTL != 5*time.Minute {
		t.Errorf("CacheTTL = %v, want 5m", opts.CacheTTL)
	}
	if opts.Concurrency != 10 {
		t.Errorf("Concurrency = %d, want 10", opts.Concurrency)
	}
}

// TestResourceKindConstants verifies the kind constants exist and are valid.
func TestResourceKindConstants(t *testing.T) {
	kinds := []ResourceKind{
		KindVirtualMachine, KindContainer, KindServerless,
		KindBucket, KindDatabase, KindVolume,
		KindVirtualNetwork, KindSubnet, KindLoadBalancer, KindFirewall,
		KindIdentity, KindAccessRole, KindServiceAccount,
		KindRegion, KindResourceGroup, KindNamespace,
		KindRepository, KindCICDService,
	}
	for _, k := range kinds {
		if !k.IsValid() {
			t.Errorf("kind %q should be valid", k)
		}
		if k.String() == "" {
			t.Errorf("kind should have non-empty string")
		}
	}

	var empty ResourceKind
	if empty.IsValid() {
		t.Error("empty kind should not be valid")
	}
}

// TestLabelRegistryBasic verifies register, lookup, and dedup.
func TestLabelRegistryBasic(t *testing.T) {
	r := NewLabelRegistry()

	r.Register("AwsEc2Instance", SourceBinding{Source: "aws", Connection: "aws_prod"})
	r.Register("VirtualMachine", SourceBinding{Source: "aws", Connection: "aws_prod"})
	r.Register("VirtualMachine", SourceBinding{Source: "gcp", Connection: "gcp_prod"})

	// Vendor label lookup.
	bindings := r.Lookup("AwsEc2Instance")
	if len(bindings) != 1 {
		t.Fatalf("AwsEc2Instance: got %d bindings, want 1", len(bindings))
	}
	if bindings[0].Source != "aws" {
		t.Errorf("source = %q, want aws", bindings[0].Source)
	}

	// Normalized kind fan-out.
	bindings = r.Lookup("VirtualMachine")
	if len(bindings) != 2 {
		t.Fatalf("VirtualMachine: got %d bindings, want 2", len(bindings))
	}

	// Unknown label.
	if r.Lookup("Unknown") != nil {
		t.Error("expected nil for unknown label")
	}

	// Dedup: register same binding again.
	r.Register("AwsEc2Instance", SourceBinding{Source: "aws", Connection: "aws_prod"})
	if len(r.Lookup("AwsEc2Instance")) != 1 {
		t.Error("duplicate registration should be ignored")
	}
}

// TestLabelRegistryRegisterResourceTypes tests bulk registration.
func TestLabelRegistryRegisterResourceTypes(t *testing.T) {
	r := NewLabelRegistry()

	types := []ResourceType{
		{Name: "aws_ec2_instance", Kind: "VirtualMachine"},
		{Name: "aws_s3_bucket", Kind: "Bucket"},
		{Name: "aws_custom_thing"}, // no normalized kind
	}

	r.RegisterResourceTypes("aws", "aws_prod", types)

	// Vendor labels should be registered.
	if r.Lookup("aws_ec2_instance") == nil {
		t.Error("aws_ec2_instance should be registered")
	}
	if r.Lookup("aws_s3_bucket") == nil {
		t.Error("aws_s3_bucket should be registered")
	}
	if r.Lookup("aws_custom_thing") == nil {
		t.Error("aws_custom_thing should be registered")
	}

	// Normalized kinds should be registered where available.
	if r.Lookup("VirtualMachine") == nil {
		t.Error("VirtualMachine should be registered")
	}
	if r.Lookup("Bucket") == nil {
		t.Error("Bucket should be registered")
	}

	// All registered labels.
	labels := r.Labels()
	if len(labels) != 5 {
		t.Errorf("got %d labels, want 5", len(labels))
	}
}

// TestLabelRegistryConcurrency verifies thread safety.
func TestLabelRegistryConcurrency(t *testing.T) {
	r := NewLabelRegistry()
	done := make(chan struct{})

	go func() {
		for i := range 100 {
			r.Register("label", SourceBinding{
				Source:     "src",
				Connection: "conn_" + string(rune('0'+i%10)),
			})
		}
		close(done)
	}()

	// Read while writing.
	for range 100 {
		_ = r.Lookup("label")
		_ = r.Labels()
	}

	<-done
}

// mockGraphBuilder is a minimal test GraphBuilder.
type mockGraphBuilder struct {
	nodes []mockNode
	edges []mockEdge
}

type mockNode struct {
	label string
	id    string
	props map[string]any
}

type mockEdge struct {
	from, to string
	relType  string
	props    map[string]any
}

func (b *mockGraphBuilder) AddNode(vendorLabel string, id string, properties map[string]any) {
	b.nodes = append(b.nodes, mockNode{label: vendorLabel, id: id, props: properties})
}

func (b *mockGraphBuilder) AddEdge(fromID string, toID string, relType string, properties map[string]any) {
	b.edges = append(b.edges, mockEdge{from: fromID, to: toID, relType: relType, props: properties})
}

// TestMockDataSourceIngest tests a mock data source emitting through GraphBuilder.
func TestMockDataSourceIngest(t *testing.T) {
	ds := &mockDataSource{
		info: DataSourceInfo{Name: "test"},
		ingestFunc: func(_ context.Context, builder GraphBuilder, _ IngestOptions) error {
			builder.AddNode("TestVM", "vm-1", map[string]any{"name": "web-1"})
			builder.AddNode("TestVM", "vm-2", map[string]any{"name": "web-2"})
			builder.AddEdge("vm-1", "vm-2", "COMMUNICATES_WITH", nil)
			return nil
		},
	}

	builder := &mockGraphBuilder{}
	if err := ds.Ingest(context.Background(), builder, IngestOptions{}); err != nil {
		t.Fatal(err)
	}

	if len(builder.nodes) != 2 {
		t.Errorf("got %d nodes, want 2", len(builder.nodes))
	}
	if len(builder.edges) != 1 {
		t.Errorf("got %d edges, want 1", len(builder.edges))
	}
	if builder.edges[0].relType != "COMMUNICATES_WITH" {
		t.Errorf("relType = %q, want COMMUNICATES_WITH", builder.edges[0].relType)
	}
}
