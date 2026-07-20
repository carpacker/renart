package runcontext

const writeResourceIdentityVersion = "renart-write-resource-v1"

// WriteResourceCoordinates are the secret-free coordinates of one exclusive
// mutation resource. FilePath is canonical before this type is constructed;
// TargetIdentity is already an opaque physical-target digest.
type WriteResourceCoordinates struct {
	Kind           string `mapstructure:"kind"`
	FilePath       string `mapstructure:"file_path"`
	TargetIdentity string `mapstructure:"target_identity"`
}

func WriteResourceIdentity(resource WriteResourceCoordinates) Identity {
	return SecretFreeCanonicalIdentity(writeResourceIdentityVersion, resource)
}
