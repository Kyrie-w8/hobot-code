package hobot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func qualificationModelIdentity(selection string) (string, string, bool) {
	provider, model, ok := strings.Cut(strings.TrimSpace(selection), "/")
	return provider, model, ok && qualificationProviderPattern.MatchString(provider) && qualificationModelPattern.MatchString(model)
}

func decodeModelRDKMatrix(raw []byte) (ModelRDKMatrix, error) {
	var result ModelRDKMatrix
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("decode RDK profile matrix: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return result, fmt.Errorf("decode RDK profile matrix: expected one JSON object")
	}
	if err := validateModelRDKMatrix(result); err != nil {
		return result, fmt.Errorf("board returned invalid RDK profile matrix: %w", err)
	}
	return result, nil
}

func validateModelRDKMatrix(result ModelRDKMatrix) error {
	if result.SchemaVersion != 1 || !qualificationProviderPattern.MatchString(result.Provider) || !qualificationModelPattern.MatchString(result.Model) ||
		!safeQualificationLabel(result.BoardID, 32) || !safeQualificationLabel(result.RDKOSVersion, 128) || !safeQualificationLabel(result.Architecture, 32) || result.CapturedAt.IsZero() ||
		len(result.Profiles) != len(qualificationRDKProfiles) {
		return fmt.Errorf("invalid matrix identity or profile count")
	}
	seen := map[string]bool{}
	for _, profile := range result.Profiles {
		notCovered, known := qualificationRDKProfiles[profile.ID]
		if !known || seen[profile.ID] || !safeQualificationLabel(profile.Name, 120) || !safeQualificationLabel(profile.Workflow, 64) || !safeQualificationLabel(profile.EvidenceClass, 64) ||
			!safeQualificationLabel(profile.Description, 512) || !oneOf(profile.Availability, "available", "planned", "unsupported-target") ||
			!oneOf(profile.EvidenceState, "untested", "current", "stale") || !equalQualificationValues(profile.Targets, []string{"x5", "s100", "s600"}) ||
			!equalQualificationValues(profile.NotCovered, notCovered) || !validQualificationCodes(profile.StaleReasons, staleReasonCodes) {
			return fmt.Errorf("invalid profile %q", profile.ID)
		}
		seen[profile.ID] = true
		if !qualificationRDKProfileRunnable[profile.ID] {
			if profile.Availability != "planned" || profile.EvidenceState != "untested" || profile.Result != nil || len(profile.StaleReasons) != 0 {
				return fmt.Errorf("planned profile %q overstates evidence", profile.ID)
			}
			continue
		}
		if profile.Availability == "planned" {
			return fmt.Errorf("runnable profile %q is marked planned", profile.ID)
		}
		switch profile.EvidenceState {
		case "untested":
			if profile.Result != nil || len(profile.StaleReasons) != 0 {
				return fmt.Errorf("untested profile %q contains evidence", profile.ID)
			}
		case "current":
			if profile.Result == nil || len(profile.StaleReasons) != 0 {
				return fmt.Errorf("current profile %q omitted evidence", profile.ID)
			}
		case "stale":
			if profile.Result == nil || len(profile.StaleReasons) == 0 {
				return fmt.Errorf("stale profile %q omitted drift evidence", profile.ID)
			}
		}
		if profile.Result != nil && (profile.Result.Provider != result.Provider || profile.Result.Model != result.Model || profile.Result.Profile != profile.ID || !validQualificationRDK(*profile.Result, result.Provider, result.Model)) {
			return fmt.Errorf("profile %q contains invalid probe evidence", profile.ID)
		}
	}
	return nil
}
