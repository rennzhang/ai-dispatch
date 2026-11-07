package setup

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func parseOMPModels(raw []byte) ([]discoveredModel, error) {
	var catalog struct {
		Models *[]struct {
			ID       string   `json:"id"`
			Provider string   `json:"provider"`
			Selector string   `json:"selector"`
			Kind     string   `json:"kind"`
			Thinking []string `json:"thinking"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil || catalog.Models == nil {
		return nil, fmt.Errorf("OMP returned an invalid model catalog")
	}
	out := make([]discoveredModel, 0, len(*catalog.Models))
	seen := map[string]bool{}
	for _, model := range *catalog.Models {
		if model.Kind != "" && model.Kind != "chat" {
			continue
		}
		id := strings.TrimSpace(model.Selector)
		if id == "" && model.Provider != "" && model.ID != "" {
			// OMP's documented exact selector is provider/id. Preserve the
			// returned provider and ID rather than mapping vendor names.
			id = model.Provider + "/" + model.ID
		}
		if id == "" {
			return nil, fmt.Errorf("OMP catalog entry has no selector")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, discoveredModel{Key: nativeModelKey("omp", id), ID: id, Efforts: supportedEfforts(model.Thinking)})
	}
	return out, nil
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
var modelToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/@+\[\]=,:-]*$`)

// Text discovery only accepts model rows, never arbitrary help or banner text.
func parseTextModels(provider string, raw []byte) ([]discoveredModel, error) {
	text := ansiEscape.ReplaceAllString(string(raw), "")
	if strings.HasPrefix(strings.TrimSpace(text), "{") || strings.HasPrefix(strings.TrimSpace(text), "[") {
		return parseJSONModelIDs(provider, []byte(text))
	}
	var out []discoveredModel
	seen := map[string]bool{}
	recognized := false
	inGrokList := provider != "grok"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.EqualFold(line, "Available models:") || strings.EqualFold(line, "Models:") {
			recognized, inGrokList = true, true
			continue
		}
		if !inGrokList {
			continue
		}
		id, ok := textModelID(line, provider)
		if !ok || seen[id] {
			continue
		}
		seen[id], recognized = true, true
		out = append(out, discoveredModel{Key: nativeModelKey(provider, id), ID: id})
	}
	if !recognized {
		return nil, fmt.Errorf("unrecognized model catalog format")
	}
	return out, nil
}

func textModelID(line, provider string) (string, bool) {
	line = strings.TrimSpace(strings.TrimLeft(line, "*-•"))
	if line == "" {
		return "", false
	}
	if id, _, ok := strings.Cut(line, "\t"); ok {
		return id, validModelID(id)
	}
	for _, separator := range []string{" - ", " — ", " – "} {
		if id, _, ok := strings.Cut(line, separator); ok {
			return id, validModelID(id)
		}
	}
	if provider == "grok" {
		id := strings.Fields(line)[0]
		return id, strings.HasPrefix(id, "grok-") && validModelID(id)
	}
	if strings.ContainsAny(line, " \t") {
		return "", false
	}
	return line, validModelID(line)
}

func validModelID(id string) bool {
	return modelToken.MatchString(id) && strings.ContainsAny(id, "-./")
}

func parseJSONModelIDs(provider string, raw []byte) ([]discoveredModel, error) {
	var records []json.RawMessage
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		if err := json.Unmarshal(raw, &records); err != nil {
			return nil, err
		}
	} else {
		var wrapper struct {
			Models []json.RawMessage `json:"models"`
		}
		if err := json.Unmarshal(raw, &wrapper); err != nil || wrapper.Models == nil {
			return nil, fmt.Errorf("invalid model catalog")
		}
		records = wrapper.Models
	}
	out := make([]discoveredModel, 0, len(records))
	seen := map[string]bool{}
	for _, record := range records {
		var id string
		if err := json.Unmarshal(record, &id); err != nil {
			var model struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(record, &model); err != nil {
				return nil, err
			}
			id = model.ID
		}
		if !validModelID(id) {
			return nil, fmt.Errorf("model catalog contains an invalid id")
		}
		if !seen[id] {
			out = append(out, discoveredModel{Key: nativeModelKey(provider, id), ID: id})
			seen[id] = true
		}
	}
	return out, nil
}
