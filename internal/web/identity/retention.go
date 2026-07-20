package identity

import "fmt"

const (
	maxRetentionDays            = 100 * 365
	maxRetentionMinimumPerGroup = 100_000
	maxTemporaryDirectoryHours  = 365 * 24
)

// RetentionWindow keeps records for both a time window and a minimum count.
// A record is eligible for deletion only after it falls outside both bounds.
type RetentionWindow struct {
	Days               int `yaml:"days"`
	MinimumPerPipeline int `yaml:"minimum_per_pipeline"`
}

// RetentionSettings is the tracked local-history policy in
// .renart/project.yml. Optional zero-valued sections are filled from
// DefaultRetentionSettings so a hand-written partial policy stays concise.
type RetentionSettings struct {
	RunMetadata               RetentionWindow `yaml:"run_metadata"`
	FullLogs                  RetentionWindow `yaml:"full_logs"`
	MaterializationFactsDays  int             `yaml:"materialization_facts_days"`
	ScheduleHistoryDays       int             `yaml:"schedule_history_days"`
	Deployments               RetentionWindow `yaml:"deployments"`
	TemporaryDirectoriesHours int             `yaml:"temporary_directories_hours"`
}

// DefaultRetentionSettings returns Renart's conservative local-history
// defaults. Keep this as a function so callers cannot mutate shared state.
func DefaultRetentionSettings() RetentionSettings {
	return RetentionSettings{
		RunMetadata: RetentionWindow{
			Days:               180,
			MinimumPerPipeline: 100,
		},
		FullLogs: RetentionWindow{
			Days:               30,
			MinimumPerPipeline: 25,
		},
		MaterializationFactsDays: 90,
		ScheduleHistoryDays:      180,
		Deployments: RetentionWindow{
			Days:               90,
			MinimumPerPipeline: 20,
		},
		TemporaryDirectoriesHours: 24,
	}
}

// NormalizeRetentionSettings fills omitted sections and validates explicit
// values. Minimum counts may be zero; time windows remain positive so a typo
// cannot turn daily housekeeping into an unbounded delete.
func NormalizeRetentionSettings(settings *RetentionSettings) (RetentionSettings, error) {
	defaults := DefaultRetentionSettings()
	if settings == nil {
		return defaults, nil
	}

	normalized := *settings
	if normalized.RunMetadata == (RetentionWindow{}) {
		normalized.RunMetadata = defaults.RunMetadata
	}
	if normalized.FullLogs == (RetentionWindow{}) {
		normalized.FullLogs = defaults.FullLogs
	}
	if normalized.MaterializationFactsDays == 0 {
		normalized.MaterializationFactsDays = defaults.MaterializationFactsDays
	}
	if normalized.ScheduleHistoryDays == 0 {
		normalized.ScheduleHistoryDays = defaults.ScheduleHistoryDays
	}
	if normalized.Deployments == (RetentionWindow{}) {
		normalized.Deployments = defaults.Deployments
	}
	if normalized.TemporaryDirectoriesHours == 0 {
		normalized.TemporaryDirectoriesHours = defaults.TemporaryDirectoriesHours
	}

	if err := validateRetentionWindow("run metadata", normalized.RunMetadata); err != nil {
		return RetentionSettings{}, err
	}
	if err := validateRetentionWindow("full logs", normalized.FullLogs); err != nil {
		return RetentionSettings{}, err
	}
	if err := validateRetentionDays("materialization facts", normalized.MaterializationFactsDays); err != nil {
		return RetentionSettings{}, err
	}
	if err := validateRetentionDays("schedule history", normalized.ScheduleHistoryDays); err != nil {
		return RetentionSettings{}, err
	}
	if err := validateRetentionWindow("deployments", normalized.Deployments); err != nil {
		return RetentionSettings{}, err
	}
	if normalized.TemporaryDirectoriesHours < 1 || normalized.TemporaryDirectoriesHours > maxTemporaryDirectoryHours {
		return RetentionSettings{}, fmt.Errorf(
			"temporary directory retention must be between 1 and %d hours",
			maxTemporaryDirectoryHours,
		)
	}
	return normalized, nil
}

func validateRetentionWindow(name string, window RetentionWindow) error {
	if err := validateRetentionDays(name, window.Days); err != nil {
		return err
	}
	if window.MinimumPerPipeline < 0 || window.MinimumPerPipeline > maxRetentionMinimumPerGroup {
		return fmt.Errorf(
			"%s minimum per pipeline must be between 0 and %d",
			name,
			maxRetentionMinimumPerGroup,
		)
	}
	return nil
}

func validateRetentionDays(name string, days int) error {
	if days < 1 || days > maxRetentionDays {
		return fmt.Errorf("%s retention must be between 1 and %d days", name, maxRetentionDays)
	}
	return nil
}
