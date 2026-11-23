//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type FixtureRevision struct {
	RevisionName string         `json:"revisionName"`
	Spec         BootConfigSpec `json:"spec"`
}
type ConfigFixtures struct {
	Series    string                     `json:"series"`
	Revisions map[string]FixtureRevision `json:"revisions"`
}
type FixtureNode struct {
	Role   string   `json:"role"`
	Xname  string   `json:"xname"`
	NID    int32    `json:"nid"`
	MAC    string   `json:"mac"`
	Groups []string `json:"groups"`
}
type NodeFixtures struct {
	Provider string        `json:"provider"`
	Nodes    []FixtureNode `json:"nodes"`
}
type FixtureAssignment struct {
	Name string         `json:"name"`
	Spec AssignmentSpec `json:"spec"`
}
type AssignmentFixtures struct {
	Assignments       []FixtureAssignment `json:"assignments"`
	ReferenceVariants ReferenceVariants   `json:"referenceVariants"`
}
type Artifact struct {
	Kernel string   `json:"kernel"`
	Initrd string   `json:"initrd"`
	Params []string `json:"params"`
}
type DeliveryStage struct {
	Stage          string `json:"stage"`
	DefaultPointer string `json:"defaultPointer"`
	LatestPointer  string `json:"latestPointer"`
	Canary         string `json:"canary"`
	Default        string `json:"default"`
	Pinned         string `json:"pinned"`
}
type ExpectedFixtures struct {
	Artifacts      map[string]Artifact `json:"artifacts"`
	DeliveryStages []DeliveryStage     `json:"deliveryStages"`
}
type Fixtures struct {
	Configs     ConfigFixtures
	Nodes       NodeFixtures
	Assignments AssignmentFixtures
	Expected    ExpectedFixtures
	Negative    NegativeFixtures
	Legacy      LegacyFixtures
	Scenarios   ScenarioFixtures
}

var ErrFixture = errors.New("incomplete fixture packet")

func (f Fixtures) Validate() error {
	if err := f.Scenarios.Validate(); err != nil {
		return err
	}
	if f.Configs.Series == "" || f.Nodes.Provider != "SMD" || len(f.Nodes.Nodes) == 0 || len(f.Assignments.Assignments) == 0 || len(f.Expected.DeliveryStages) == 0 || len(f.Negative.Cases) == 0 || f.Legacy.Source == "" || len(f.Scenarios.Scenarios) == 0 || f.Assignments.ReferenceVariants.UIDA.Bind["/uid"] == "" {
		return ErrFixture
	}
	for _, name := range []string{"A", "B"} {
		revision, foundRevision := f.Configs.Revisions[name]
		artifact, foundArtifact := f.Expected.Artifacts[name]
		if !foundRevision || revision.RevisionName == "" || revision.Spec.Kernel == "" || !foundArtifact || artifact.Kernel == "" || artifact.Initrd == "" || len(artifact.Params) == 0 {
			return ErrFixture
		}
	}
	return nil
}

func LoadFixtures(dir string) (Fixtures, error) {
	var out Fixtures
	files := []struct {
		name   string
		target func([]byte) error
	}{
		{"configs.json", func(data []byte) error { return json.Unmarshal(data, &out.Configs) }},
		{"nodes.json", func(data []byte) error { return json.Unmarshal(data, &out.Nodes) }},
		{"assignments.json", func(data []byte) error { return json.Unmarshal(data, &out.Assignments) }},
		{"expected.json", func(data []byte) error { return json.Unmarshal(data, &out.Expected) }},
		{"negative.json", func(data []byte) error { return json.Unmarshal(data, &out.Negative) }},
		{"legacy.json", func(data []byte) error { return json.Unmarshal(data, &out.Legacy) }},
		{"scenarios.json", func(data []byte) error { return json.Unmarshal(data, &out.Scenarios) }},
	}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, file.name))
		if err != nil {
			return Fixtures{}, fmt.Errorf("read fixture %s: %w", file.name, err)
		}
		if err := file.target(data); err != nil {
			return Fixtures{}, fmt.Errorf("decode fixture %s: %w", file.name, err)
		}
	}
	if err := out.Validate(); err != nil {
		return Fixtures{}, err
	}
	return out, nil
}
