package core

import "time"

type sortMetadataState struct {
	VIPChecked   bool      `json:"vipChecked,omitempty"`
	Version      int       `json:"version"`
	CheckedAt    time.Time `json:"checkedAt"`
	Pending      bool      `json:"pending,omitempty"`
	CoverChecked bool      `json:"coverChecked,omitempty"`
}
