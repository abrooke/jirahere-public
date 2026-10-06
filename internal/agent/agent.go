package agent

import (
	"embed"
	"fmt"
)

//go:embed assets/*
var embeddedAssets embed.FS

var assetFilenames = map[string]map[string]string{
	"jirasistant": {
		"claude": "jirasistant.md",
		"codex":  "jirasistant.config.toml",
	},
	"jirasistant-req": {
		"claude": "jirasistant-req.md",
		"codex":  "jirasistant-req.config.toml",
	},
}

func Asset(agent, app string) (filename string, contents []byte, err error) {
	apps, ok := assetFilenames[agent]
	if !ok {
		return "", nil, fmt.Errorf("unsupported agent %q", agent)
	}
	filename, ok = apps[app]
	if !ok {
		return "", nil, fmt.Errorf("unsupported agent app %q", app)
	}

	assetPath := "assets/" + app + "/" + filename
	contents, err = embeddedAssets.ReadFile(assetPath)
	if err != nil {
		return "", nil, fmt.Errorf("read embedded agent asset %q: %w", assetPath, err)
	}
	return filename, contents, nil
}
