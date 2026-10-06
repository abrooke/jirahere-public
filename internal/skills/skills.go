package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

//go:embed assets/*
var embeddedAssets embed.FS

type Skill struct {
	Name  string
	Files map[string][]byte
}

func List() ([]Skill, error) {
	entries, err := embeddedAssets.ReadDir("assets")
	if err != nil {
		return nil, fmt.Errorf("read embedded skill assets: %w", err)
	}

	skills := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("embedded skill asset %q is not a directory", entry.Name())
		}

		name := entry.Name()
		files := make(map[string][]byte)
		root := path.Join("assets", name)
		if err := fs.WalkDir(embeddedAssets, root, func(assetPath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("%q is not a regular file", assetPath)
			}
			contents, err := embeddedAssets.ReadFile(assetPath)
			if err != nil {
				return err
			}
			files[strings.TrimPrefix(assetPath, root+"/")] = contents
			return nil
		}); err != nil {
			return nil, fmt.Errorf("read embedded skill %q: %w", name, err)
		}
		skills = append(skills, Skill{Name: name, Files: files})
	}

	return skills, nil
}

var JirasistantReqExtraSkills = []string{
	"req-create",
	"req-done",
	"req-close",
	"req-update-progress",
	"req-update-decisions",
}

func DefaultSet(all []Skill) []Skill {
	var out []Skill
	for _, s := range all {
		if strings.HasPrefix(s.Name, "jirahere-") {
			out = append(out, s)
		}
	}
	return out
}

func JirasistantReqSet(all []Skill) ([]Skill, error) {
	byName := make(map[string]Skill, len(all))
	for _, s := range all {
		byName[s.Name] = s
	}

	var missing []string
	out := DefaultSet(all)
	for _, name := range JirasistantReqExtraSkills {
		s, ok := byName[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		out = append(out, s)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing embedded skill(s) required for jirasistant-req: %s", strings.Join(missing, ", "))
	}
	return out, nil
}
