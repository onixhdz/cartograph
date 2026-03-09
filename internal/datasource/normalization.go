package datasource

// ResourceKind is a normalized resource type that abstracts across cloud
// providers and SaaS platforms. For example, AWS EC2 Instance, Azure VM,
// and GCP Compute Instance all normalize to KindVirtualMachine.
//
// Plugins emit vendor-specific labels (e.g., "AwsEc2Instance"); the host
// maps them to a ResourceKind and applies it as an additional graph label.
// This enables cross-provider queries like "show all VirtualMachines".
//
// The full normalization mapping (~160 kinds, ~4000 vendor labels) is
// maintained in cloudgraph-normalization-data.md. The constants below
// cover the most common kinds used in cross-provider queries.
type ResourceKind string

// Compute resources.
const (
	KindVirtualMachine    ResourceKind = "VirtualMachine"
	KindContainer         ResourceKind = "Container"
	KindContainerService  ResourceKind = "ContainerService"
	KindServerless        ResourceKind = "Serverless"
	KindKubernetesCluster ResourceKind = "KubernetesCluster"
	KindPod               ResourceKind = "Pod"
	KindDeployment        ResourceKind = "Deployment"
)

// Storage resources.
const (
	KindBucket         ResourceKind = "Bucket"
	KindDatabase       ResourceKind = "Database"
	KindDatabaseServer ResourceKind = "DatabaseServer"
	KindVolume         ResourceKind = "Volume"
	KindSnapshot       ResourceKind = "Snapshot"
	KindStorageAccount ResourceKind = "StorageAccount"
)

// Networking resources.
const (
	KindVirtualNetwork   ResourceKind = "VirtualNetwork"
	KindSubnet           ResourceKind = "Subnet"
	KindLoadBalancer     ResourceKind = "LoadBalancer"
	KindFirewall         ResourceKind = "Firewall"
	KindDNSZone          ResourceKind = "DNSZone"
	KindDNSRecord        ResourceKind = "DNSRecord"
	KindCDN              ResourceKind = "CDN"
	KindAPIGateway       ResourceKind = "APIGateway"
	KindNAT              ResourceKind = "NAT"
	KindNetworkAddress   ResourceKind = "NetworkAddress"
	KindNetworkAppliance ResourceKind = "NetworkAppliance"
)

// Identity and access resources.
const (
	KindIdentity       ResourceKind = "Identity"
	KindAccessRole     ResourceKind = "AccessRole"
	KindServiceAccount ResourceKind = "ServiceAccount"
	KindAccessKey      ResourceKind = "AccessKey"
	KindGroup          ResourceKind = "Group"
	KindSecret         ResourceKind = "Secret"
	KindEncryptionKey  ResourceKind = "EncryptionKey"
	KindUserAccount    ResourceKind = "UserAccount"
)

// Platform and management resources.
const (
	KindRegion         ResourceKind = "Region"
	KindResourceGroup  ResourceKind = "ResourceGroup"
	KindSubscription   ResourceKind = "Subscription"
	KindNamespace      ResourceKind = "Namespace"
	KindMonitorService ResourceKind = "MonitorService"
	KindMonitorAlert   ResourceKind = "MonitorAlert"
)

// Development and CI/CD resources.
const (
	KindRepository  ResourceKind = "Repository"
	KindCICDService ResourceKind = "CICDService"
	KindCIWorkflow  ResourceKind = "CIWorkflow"
	KindCIJob       ResourceKind = "CIJob"
)

// AI/ML resources.
const (
	KindAIModel   ResourceKind = "AIModel"
	KindAIService ResourceKind = "AIService"
)

// IsValid reports whether the kind is a non-empty string.
func (k ResourceKind) IsValid() bool { return k != "" }

// String returns the kind as a string.
func (k ResourceKind) String() string { return string(k) }
