//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

type BoundReference struct {
	Value ConfigReference   `json:"value"`
	Bind  map[string]string `json:"bind"`
}
type ReferenceVariants struct {
	BareDefault     ConfigReference `json:"bareDefault"`
	ExplicitDefault ConfigReference `json:"explicitDefault"`
	Latest          ConfigReference `json:"latest"`
	NamedA          ConfigReference `json:"namedA"`
	UIDA            BoundReference  `json:"uidA"`
}
type NegativeCase struct {
	ID                  string            `json:"id"`
	Classification      string            `json:"classification"`
	Method              string            `json:"method,omitempty"`
	Path                string            `json:"path,omitempty"`
	Spec                *AssignmentSpec   `json:"spec,omitempty"`
	Body                *BootAssignment   `json:"body,omitempty"`
	RawBody             string            `json:"rawBody,omitempty"`
	Bind                map[string]string `json:"bind,omitempty"`
	ExpectedStatus      int               `json:"expectedStatus,omitempty"`
	ExpectedStatusClass int               `json:"expectedStatusClass,omitempty"`
	Unchanged           bool              `json:"unchanged"`
}
type NegativeFixtures struct {
	Cases []NegativeCase `json:"cases"`
}
type LegacyContent struct {
	Kernel string `json:"Kernel"`
	Initrd string `json:"Initrd"`
	Params string `json:"Params"`
}
type LegacyAdoption struct {
	Series           string `json:"series"`
	Assignment       string `json:"assignment"`
	SourceUIDBinding string `json:"sourceUIDBinding"`
	RevisionNameRule string `json:"revisionNameRule"`
}
type LegacyFixtures struct {
	Source   string         `json:"source"`
	Node     string         `json:"node"`
	Default  LegacyContent  `json:"default"`
	Adoption LegacyAdoption `json:"adoption"`
}
type ExecutionPolicy struct {
	Enabled                     bool   `json:"enabled"`
	RequiredResult              string `json:"requiredResult"`
	SkipAllowed                 bool   `json:"skipAllowed"`
	MissingScenarioIsFailure    bool   `json:"missingScenarioIsFailure"`
	UnexpectedScenarioIsFailure bool   `json:"unexpectedScenarioIsFailure"`
}
type Scenario struct {
	ID      string `json:"id"`
	Clauses []int  `json:"clauses"`
	Expect  string `json:"expect"`
}
type ScenarioFixtures struct {
	Version         int             `json:"version"`
	ExecutionPolicy ExecutionPolicy `json:"executionPolicy"`
	Scenarios       []Scenario      `json:"scenarios"`
}

func (s ScenarioFixtures) Validate() error {
	policy := s.ExecutionPolicy
	if s.Version != 1 || !policy.Enabled || policy.RequiredResult != "PASS" || policy.SkipAllowed || !policy.MissingScenarioIsFailure || !policy.UnexpectedScenarioIsFailure || len(s.Scenarios) != 36 {
		return ErrFixture
	}
	seen := make(map[string]bool, len(s.Scenarios))
	for _, scenario := range s.Scenarios {
		if scenario.ID == "" || scenario.Expect == "" || len(scenario.Clauses) == 0 || seen[scenario.ID] {
			return ErrFixture
		}
		seen[scenario.ID] = true
	}
	return nil
}
