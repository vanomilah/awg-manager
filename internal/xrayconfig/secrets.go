package xrayconfig

import "context"

// SecretResolver resolves a secret reference into its cleartext value.
type SecretResolver interface {
	ResolveSecret(ctx context.Context, ref SecretRef) (string, error)
}

// SecretStager stages a secret within an active transaction.
type SecretStager interface {
	SecretResolver
	StageSecret(ctx context.Context, txID string, secretType string, name string, value string) (SecretRef, error)
}
