package runcontext

const physicalTargetIdentityVersion = "renart-physical-target-v1"

// PhysicalTargetCoordinates is the deliberately small, secret-free projection
// used to identify one mutable physical output. Connection aliases, environment
// names, principals, and credentials do not belong here: two aliases that route
// to the same endpoint and object must produce the same identity.
//
// Callers must resolve defaults and reject opaque routing controls before
// constructing this value. SecretFreeCanonicalIdentity then provides the final
// structural fail-closed guard and versioned digest.
type PhysicalTargetCoordinates struct {
	Kind        string `mapstructure:"kind"`
	Platform    string `mapstructure:"platform"`
	Host        string `mapstructure:"host"`
	Port        int    `mapstructure:"port"`
	Account     string `mapstructure:"account"`
	Region      string `mapstructure:"region"`
	RoutingPath string `mapstructure:"routing_path"`
	Catalog     string `mapstructure:"catalog"`
	Schema      string `mapstructure:"schema"`
	Object      string `mapstructure:"object"`
	FilePath    string `mapstructure:"file_path"`
}

// PhysicalTargetIdentity returns a stable identity for already-resolved target
// coordinates. It intentionally returns only a digest; callers retain a
// separate, safe display object and must never expose endpoint coordinates.
func PhysicalTargetIdentity(target PhysicalTargetCoordinates) Identity {
	return SecretFreeCanonicalIdentity(physicalTargetIdentityVersion, target)
}
