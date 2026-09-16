package xrayconfig

import (
	"fmt"
)

// MigrationFunc transforms a ManagedConfig from one schema version to the next.
type MigrationFunc func(cfg *ManagedConfig) (*ManagedConfig, error)

var schemaMigrations = map[int]MigrationFunc{
	// Future migrations:
	// 1: migrateV1ToV2,
}

// MigrateConfig brings a ManagedConfig from its current schema version to targetVersion.
// If targetVersion is 0, CurrentSchemaVersion is used.
func MigrateConfig(cfg *ManagedConfig, fromVersion, targetVersion int) (*ManagedConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cannot migrate nil config")
	}

	if targetVersion == 0 {
		targetVersion = CurrentSchemaVersion
	}

	if fromVersion > CurrentSchemaVersion {
		return nil, fmt.Errorf("unsupported future schema version %d (current supported: %d)", fromVersion, CurrentSchemaVersion)
	}

	if targetVersion > CurrentSchemaVersion {
		return nil, fmt.Errorf("target schema version %d exceeds current supported version %d", targetVersion, CurrentSchemaVersion)
	}

	if fromVersion == targetVersion {
		return cfg, nil
	}

	if fromVersion > targetVersion {
		return nil, fmt.Errorf("downgrading schema version from %d to %d is not supported", fromVersion, targetVersion)
	}

	current := cfg
	for v := fromVersion; v < targetVersion; v++ {
		migrator, exists := schemaMigrations[v]
		if !exists {
			// If no explicit migration func is defined for v -> v+1 and v < targetVersion,
			// check if standard forward-compatible defaults suffice.
			// Currently schema 1 is the initial managed schema.
			return nil, fmt.Errorf("no migration step registered for version %d to %d", v, v+1)
		}
		var err error
		current, err = migrator(current)
		if err != nil {
			return nil, fmt.Errorf("migration from version %d to %d failed: %w", v, v+1, err)
		}
	}

	return current, nil
}
