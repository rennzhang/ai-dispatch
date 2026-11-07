package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DesktopModel is a model advertised by one Codex backend. It is a catalog
// entry, not proof that a GUI preset or an inference request will succeed.
type DesktopModel struct {
	ID              string
	Name            string
	Efforts         []string
	InputModalities []string
	Default         bool
}

// DesktopExecutable locates the installed bundled CLI without opening the App.
func DesktopExecutable(ctx context.Context) (string, error) {
	host, err := registeredDesktopHost(ctx)
	return host.binary, err
}

// DiscoverDesktopModels reads the installed Desktop's bundled backend only.
// It does not open the GUI, create a thread, or send a model request.
func DiscoverDesktopModels(ctx context.Context) ([]DesktopModel, error) {
	host, err := registeredDesktopHost(ctx)
	if err != nil {
		return nil, err
	}
	return DiscoverCLIModels(ctx, host.binary)
}

// DiscoverCLIModels reads a specific Codex binary's model/list catalog. Calling
// this separately for PATH and bundled binaries keeps their catalogs distinct.
func DiscoverCLIModels(ctx context.Context, binary string) ([]DesktopModel, error) {
	client, err := startAppServer(ctx, desktopHost{binary: binary})
	if err != nil {
		return nil, err
	}
	defer client.Close()
	if err := client.initialize(ctx); err != nil {
		return nil, err
	}
	var models []DesktopModel
	cursor := ""
	seen := make(map[string]bool)
	for {
		params := map[string]any{"limit": 100, "includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := client.call(ctx, "model/list", params)
		if err != nil {
			return nil, err
		}
		page, next, err := decodeModelPage(raw)
		if err != nil {
			return nil, err
		}
		models = append(models, page...)
		if next == "" {
			return models, nil
		}
		if seen[next] {
			return nil, fmt.Errorf("Codex model/list repeated pagination cursor")
		}
		seen[next], cursor = true, next
	}
}

func decodeModelPage(raw json.RawMessage) ([]DesktopModel, string, error) {
	var page struct {
		Data []struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Name    string `json:"displayName"`
			Hidden  bool   `json:"hidden"`
			Default bool   `json:"isDefault"`
			Efforts []struct {
				Effort string `json:"reasoningEffort"`
			} `json:"supportedReasoningEfforts"`
			Modalities []string `json:"inputModalities"`
		} `json:"data"`
		Next string `json:"nextCursor"`
	}
	if err := json.Unmarshal(raw, &page); err != nil || page.Data == nil {
		return nil, "", fmt.Errorf("Codex model/list returned an invalid catalog")
	}
	models := make([]DesktopModel, 0, len(page.Data))
	for _, item := range page.Data {
		id := strings.TrimSpace(firstNonEmpty(item.Model, item.ID))
		if item.Hidden || id == "" {
			continue
		}
		model := DesktopModel{ID: id, Name: item.Name, InputModalities: item.Modalities, Default: item.Default}
		for _, effort := range item.Efforts {
			if effort.Effort != "" {
				model.Efforts = append(model.Efforts, effort.Effort)
			}
		}
		models = append(models, model)
	}
	return models, page.Next, nil
}
