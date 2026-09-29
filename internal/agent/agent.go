package agent

import (
	"embed"
	"fmt"
)

//go:embed assets/*
var embeddedAssets embed.FS

const (
	claudeFilename = "jirasistant.md"
	codexFilename  = "jirasistant.config.toml"
)

func Asset(app string) (filename string, contents []byte, err error) {
	var assetPath string
	switch app {
	case "claude":
		filename, assetPath = claudeFilename, "assets/claude/"+claudeFilename
	case "codex":
		filename, assetPath = codexFilename, "assets/codex/"+codexFilename
	default:
		return "", nil, fmt.Errorf("unsupported agent app %q", app)
	}

	contents, err = embeddedAssets.ReadFile(assetPath)
	if err != nil {
		return "", nil, fmt.Errorf("read embedded agent asset %q: %w", assetPath, err)
	}
	return filename, contents, nil
}
