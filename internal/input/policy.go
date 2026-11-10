package input

import (
	"fmt"
	"os"

	"tracey-sof/internal/model"
)

func DefaultTracePolicy() model.TracePolicy {
	return model.TracePolicy{
		SchemaVersion:   model.SchemaVersion,
		MaxHops:         model.DefaultMaxHops,
		MaxTransactions: model.DefaultMaxTransactions,
	}
}

// LoadTracePolicy returns defaults when path does not exist. The policy file is
// optional so existing owned/purchases/annotations workflows remain unchanged.
func LoadTracePolicy(path string) (model.TracePolicy, error) {
	policy := DefaultTracePolicy()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return policy, nil
		}
		return policy, fmt.Errorf("stat trace policy %s: %w", path, err)
	}
	if err := loadJSON(path, &policy); err != nil {
		return policy, err
	}
	if err := ValidateTracePolicy(policy); err != nil {
		return policy, fmt.Errorf("validate %s: %w", path, err)
	}
	return policy, nil
}

func ValidateTracePolicy(policy model.TracePolicy) error {
	if policy.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", policy.SchemaVersion)
	}
	if policy.MaxHops < 0 || policy.MaxHops > 10000 {
		return fmt.Errorf("max_hops must be between 0 and 10000")
	}
	if policy.MaxTransactions < 0 || policy.MaxTransactions > 1000000 {
		return fmt.Errorf("max_transactions must be between 0 and 1000000")
	}
	if policy.StopIfPrevoutValueSatsGTE < 0 || policy.StopIfPrevoutValueSatsGTE > 2100000000000000 {
		return fmt.Errorf("stop_if_prevout_value_sats_gte must be between 0 and 2100000000000000")
	}
	if policy.StopIfInputCountGTE < 0 || policy.StopIfInputCountGTE > 1000000 {
		return fmt.Errorf("stop_if_input_count_gte must be between 0 and 1000000")
	}
	if policy.StopIfOutputCountGTE < 0 || policy.StopIfOutputCountGTE > 1000000 {
		return fmt.Errorf("stop_if_output_count_gte must be between 0 and 1000000")
	}
	if policy.StopBeforeBlockHeight < 0 {
		return fmt.Errorf("stop_before_block_height cannot be negative")
	}
	return nil
}
