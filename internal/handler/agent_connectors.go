package handler

import (
	"encoding/json"
	"github.com/calnode/calnode/internal/agentconnectors"
	"net/http"
	"strings"
)

func (h *Handler) AgentConnectors(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]any{
		"items":      json.RawMessage(agentconnectors.Catalog),
		"mcp_url":    strings.TrimRight(h.baseURL, "/") + "/mcp",
		"checked_at": "2026-10-12",
	})
}
