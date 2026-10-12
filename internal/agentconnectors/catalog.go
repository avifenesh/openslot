// Package agentconnectors describes supported client setup formats.
package agentconnectors

import (
	_ "embed"
	"encoding/json"
)

//go:embed catalog.json
var Catalog []byte

func Valid(id string) bool {
	if id == "" {
		return true
	}
	var entries []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(Catalog, &entries) != nil {
		return false
	}
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}
