// Package datasource defines the core interfaces for external data sources
// that emit nodes and edges into the Cartograph graph. These interfaces are
// transport-agnostic — they are satisfied by both in-process data sources
// and process-based JSON-RPC 2.0 plugins.
package datasource

import (
	"context"
	"time"
)

// DataSource represents an external system that can enumerate resources
// and their relationships (e.g., a cloud provider, SaaS platform, database).
type DataSource interface {
	// Info returns metadata about this data source.
	Info() DataSourceInfo
	// Configure applies configuration (e.g., credentials, org, filters).
	// Called once before Ingest.
	Configure(config map[string]any) error
	// Ingest enumerates resources from the external system and emits them
	// into the provided GraphBuilder. It is the data source's main entry
	// point — the host calls this after Configure.
	Ingest(ctx context.Context, builder GraphBuilder, opts IngestOptions) error
	// ResourceTypes returns the set of resource types this source can provide.
	// Used for discovery and filtering.
	ResourceTypes() []ResourceType
}

// DataSourceInfo describes a data source for registration and display.
type DataSourceInfo struct {
	// Name is a short, unique identifier for the data source (e.g., "github", "aws").
	Name string
	// Description is a human-readable summary.
	Description string
	// Version is the data source (or plugin) version.
	Version string
}

// GraphBuilder is the write-only interface that data sources use to emit
// graph elements. Sources cannot read or query the graph — they only emit.
// The host applies normalized labels automatically at emit time.
type GraphBuilder interface {
	// AddNode emits a node with the given vendor label, unique ID, and properties.
	// The vendor label (e.g., "AwsEc2Instance") identifies the resource type
	// within this data source. The host maps it to a normalized kind automatically.
	AddNode(vendorLabel string, id string, properties map[string]any)
	// AddEdge emits a directed edge between two nodes identified by their IDs.
	// relType is the relationship type (e.g., "CONTAINS", "DEPENDS_ON").
	AddEdge(fromID string, toID string, relType string, properties map[string]any)
}

// IngestOptions controls the behavior of a data source ingestion.
type IngestOptions struct {
	// ResourceTypes limits ingestion to these resource types. Empty means all.
	ResourceTypes []string
	// CacheTTL is how long cached data is considered fresh. Zero means no caching.
	CacheTTL time.Duration
	// Concurrency is the maximum number of concurrent API calls. Zero means
	// the data source's default.
	Concurrency int
	// Quals are push-down filters (e.g., region, tags) that the data source
	// can use to reduce the scope of enumeration.
	Quals map[string]any
}

// ResourceType describes a type of resource that a data source can provide.
type ResourceType struct {
	// Name is the vendor-specific resource type name (e.g., "aws_ec2_instance").
	Name string
	// Kind is the normalized kind (e.g., "VirtualMachine"). Empty means
	// no normalization is available for this resource type.
	Kind string
	// Description is a human-readable summary of what this resource type is.
	Description string
	// KeyColumns are the columns that uniquely identify a resource of this type.
	KeyColumns []string
	// ListFilters are the supported push-down filter keys for listing.
	ListFilters []string
}
