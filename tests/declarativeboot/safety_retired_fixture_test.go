//go:build integration && declarative_boot

// Copyright © 2026 OpenCHAMI a Series of LF Projects, LLC
// SPDX-License-Identifier: MIT

package declarativeboot

import (
	"encoding/json"
	"net/http"
	"os/exec"
)

type safetyRetiredReference struct {
	SeriesName  string `json:"seriesName"`
	RevisionUID string `json:"revisionUid"`
}

func (f *safetyFixture) retiredReference() safetyRetiredReference {
	f.t.Helper()
	bin := safetyEnv(f.t, "SBX_DECLARATIVE_CONTROL_BIN")
	output, err := exec.CommandContext(f.ctx, bin, "retired-reference-fixture", "{}").Output()
	if err != nil {
		f.t.Fatal("retired reference fixture control failed")
	}
	var result safetyRetiredReference
	if json.Unmarshal(output, &result) != nil || result.SeriesName == "" || result.RevisionUID == "" {
		f.t.Fatal("retired reference fixture returned invalid identity")
	}
	response, err := f.client.ListRevisions(f.ctx, result.SeriesName)
	safetyStatus(f.t, response, err, http.StatusOK)
	records, err := DecodeRevisions(response)
	if err != nil {
		f.t.Fatal("fixture revision list is invalid")
	}
	found := false
	for _, record := range records {
		if record.Metadata.UID == result.RevisionUID && record.Metadata.SeriesName == result.SeriesName && record.Metadata.RetiredAt != nil {
			found = true
		}
	}
	if !found {
		f.t.Fatal("fixture revision is not the reported retired record")
	}
	return result
}
