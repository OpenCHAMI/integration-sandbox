//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import "encoding/json"

type Metadata struct {
	Name string `json:"name,omitempty"`
	UID  string `json:"uid,omitempty"`
}
type BootConfigSpec struct {
	Kernel string `json:"kernel"`
	Initrd string `json:"initrd,omitempty"`
	Params string `json:"params,omitempty"`
}
type BootConfig struct {
	Metadata    Metadata          `json:"metadata"`
	Spec        BootConfigSpec    `json:"spec"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Status      json.RawMessage   `json:"status,omitempty"`
}
type ConfigReference struct {
	Name         string `json:"name"`
	UID          string `json:"uid,omitempty"`
	RevisionName string `json:"revisionName,omitempty"`
	Selector     string `json:"selector,omitempty"`
}
type Selector struct {
	MACs   []string `json:"macs,omitempty"`
	NIDs   []int32  `json:"nids,omitempty"`
	Xnames []string `json:"xnames,omitempty"`
	Groups []string `json:"groups,omitempty"`
}
type AssignmentSpec struct {
	Selector  *Selector       `json:"selector,omitempty"`
	Profile   string          `json:"profile,omitempty"`
	Priority  *int            `json:"priority,omitempty"`
	ConfigRef ConfigReference `json:"configRef"`
}
type BootAssignment struct {
	Metadata Metadata        `json:"metadata"`
	Spec     AssignmentSpec  `json:"spec"`
	Status   json.RawMessage `json:"status,omitempty"`
}
