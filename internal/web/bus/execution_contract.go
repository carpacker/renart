package bus

import (
	"errors"
	"fmt"
	"strings"
)

// ExecutionContractIsEmpty distinguishes pre-v4 completion evidence from a
// version-four contract without treating a partially populated contract as
// absent.
func ExecutionContractIsEmpty(contract ExecutionContractSnapshot) bool {
	return contract.AssetID == "" &&
		contract.AssetName == "" &&
		len(contract.ConnectionKeys) == 0 &&
		contract.MutationResources.Isolation == "" &&
		len(contract.MutationResources.Claims) == 0 &&
		contract.CoordinationResources.Isolation == "" &&
		len(contract.CoordinationResources.Claims) == 0
}

// ValidateExecutionContract checks the secret-free per-asset contract carried
// by version-four completion evidence. Callers retain ownership of their
// envelope-specific error type and context.
func ValidateExecutionContract(
	assetName string,
	entry ExecutionTargetSnapshotEntry,
) error {
	contract := entry.ExecutionContract
	if contract.AssetID != entry.AssetID || contract.AssetName != assetName {
		return errors.New("mismatched asset identity")
	}
	if err := validateExecutionConnectionKeys(contract.ConnectionKeys); err != nil {
		return err
	}
	if err := validateExecutionResources(contract.MutationResources); err != nil {
		return fmt.Errorf("mutation resources: %w", err)
	}
	if err := validateExecutionResources(contract.CoordinationResources); err != nil {
		return fmt.Errorf("coordination resources: %w", err)
	}
	expectedMutation := executionTargetMutationResources(entry)
	if !equalExecutionResources(expectedMutation, contract.MutationResources) {
		return errors.New("mutation resources do not match the execution target")
	}
	if contract.MutationResources.Isolation == "pipeline" {
		if contract.CoordinationResources.Isolation != "pipeline" {
			return errors.New("pipeline mutation is not pipeline-coordinated")
		}
		return nil
	}
	if contract.CoordinationResources.Isolation != "resources" {
		return errors.New("exact mutation is not resource-coordinated")
	}
	coordination := make(map[string]struct{}, len(contract.CoordinationResources.Claims))
	for _, claim := range contract.CoordinationResources.Claims {
		coordination[claim.Kind+"\x00"+claim.Identity] = struct{}{}
	}
	for _, claim := range contract.MutationResources.Claims {
		if _, exists := coordination[claim.Kind+"\x00"+claim.Identity]; !exists {
			return errors.New("coordination omits a mutation resource")
		}
	}
	return nil
}

func validateExecutionConnectionKeys(keys []string) error {
	previous := ""
	for index, key := range keys {
		if !validExecutionDigest(key) {
			return fmt.Errorf("connection key %d is not a lowercase SHA-256 digest", index)
		}
		if index > 0 && key <= previous {
			return errors.New("connection keys must be sorted and unique")
		}
		previous = key
	}
	return nil
}

func validateExecutionResources(resources ExecutionResources) error {
	switch resources.Isolation {
	case "resources", "pipeline":
	default:
		return fmt.Errorf("unsupported isolation %q", resources.Isolation)
	}
	previous := ""
	for index, claim := range resources.Claims {
		if claim.Kind != strings.TrimSpace(claim.Kind) ||
			claim.Identity != strings.TrimSpace(claim.Identity) {
			return fmt.Errorf("resource claim %d is not canonical", index)
		}
		switch claim.Kind {
		case "local_file", "duckdb_database", "warehouse_relation":
		default:
			return fmt.Errorf("resource claim %d has unsupported kind %q", index, claim.Kind)
		}
		if !validExecutionDigest(claim.Identity) {
			return fmt.Errorf("resource claim %d identity is not a lowercase SHA-256 digest", index)
		}
		key := claim.Kind + "\x00" + claim.Identity
		if previous != "" && key <= previous {
			return errors.New("resource claims must be sorted and unique")
		}
		previous = key
	}
	return nil
}

func executionTargetMutationResources(entry ExecutionTargetSnapshotEntry) ExecutionResources {
	if entry.WriteResourceFidelity != "exact" {
		return ExecutionResources{Isolation: "pipeline", Claims: []ExecutionResourceClaim{}}
	}
	if entry.WriteResourceKind == "none" {
		return ExecutionResources{Isolation: "resources", Claims: []ExecutionResourceClaim{}}
	}
	return ExecutionResources{
		Isolation: "resources",
		Claims: []ExecutionResourceClaim{{
			Kind: entry.WriteResourceKind, Identity: entry.WriteResourceIdentity,
		}},
	}
}

func equalExecutionResources(left, right ExecutionResources) bool {
	if left.Isolation != right.Isolation || len(left.Claims) != len(right.Claims) {
		return false
	}
	for index := range left.Claims {
		if left.Claims[index] != right.Claims[index] {
			return false
		}
	}
	return true
}

func validExecutionDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
