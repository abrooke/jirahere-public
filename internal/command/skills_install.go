package command

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aslanbrooke/jirahere/internal/layout"
	"github.com/aslanbrooke/jirahere/internal/safedelete"
	"github.com/aslanbrooke/jirahere/internal/skills"
)

type SkillsInstallResult struct {
	Written  []string
	Replaced []string
	Pruned   []string
	Events   []SkillsInstallEvent
}

type SkillsInstallEvent struct {
	Action string
	Name   string
}

type SkillsInstallError struct {
	Skill string
	Err   error
}

func (e *SkillsInstallError) Error() string { return fmt.Sprintf("skill %q: %v", e.Skill, e.Err) }
func (e *SkillsInstallError) Unwrap() error { return e.Err }

func InstallSkills(target string, bundled []skills.Skill, prune bool) (SkillsInstallResult, error) {
	if err := ensureSkillsTarget(target); err != nil {
		return SkillsInstallResult{}, err
	}

	sort.Slice(bundled, func(i, j int) bool { return bundled[i].Name < bundled[j].Name })
	shipped := make(map[string]struct{}, len(bundled))
	result := SkillsInstallResult{}
	for _, skill := range bundled {
		if err := validateSkill(skill); err != nil {
			return result, &SkillsInstallError{Skill: skill.Name, Err: err}
		}
		if _, exists := shipped[skill.Name]; exists {
			return result, &SkillsInstallError{Skill: skill.Name, Err: fmt.Errorf("duplicate bundled skill")}
		}
		shipped[skill.Name] = struct{}{}

		replaced, err := installSkill(target, skill)
		if err != nil {
			return result, &SkillsInstallError{Skill: skill.Name, Err: err}
		}
		if replaced {
			result.Replaced = append(result.Replaced, skill.Name)
			result.Events = append(result.Events, SkillsInstallEvent{Action: "replaced", Name: skill.Name})
		} else {
			result.Written = append(result.Written, skill.Name)
			result.Events = append(result.Events, SkillsInstallEvent{Action: "written", Name: skill.Name})
		}
	}

	if prune {
		entries, err := os.ReadDir(target)
		if err != nil {
			return result, fmt.Errorf("read target: %w", err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !hasOwnedPrefix(name) {
				continue
			}
			if _, current := shipped[name]; current || !entry.IsDir() {
				continue
			}

			pruneTarget := filepath.Join(target, name)
			if !strings.Contains(name, layout.Namespace) {

				renamed := filepath.Join(target, SkillPreviousPrefix+name)

				if _, statErr := os.Lstat(renamed); statErr == nil {
					if err := safedelete.RemoveAll(target, renamed); err != nil {
						return result, &SkillsInstallError{Skill: name, Err: fmt.Errorf("clear existing rename destination: %w", err)}
					}
				} else if !os.IsNotExist(statErr) {
					return result, &SkillsInstallError{Skill: name, Err: fmt.Errorf("inspect rename destination: %w", statErr)}
				}
				if err := os.Rename(pruneTarget, renamed); err != nil {
					return result, &SkillsInstallError{Skill: name, Err: fmt.Errorf("rename for safe deletion: %w", err)}
				}
				pruneTarget = renamed
			}
			if err := safedelete.RemoveAll(target, pruneTarget); err != nil {
				return result, &SkillsInstallError{Skill: name, Err: err}
			}
			result.Pruned = append(result.Pruned, name)
			result.Events = append(result.Events, SkillsInstallEvent{Action: "pruned", Name: name})
		}
	}
	return result, nil
}

func ensureSkillsTarget(target string) error {
	missing := []string{}
	for current := filepath.Clean(target); ; current = filepath.Dir(current) {
		_, err := os.Lstat(current)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("inspect target: %w", err)
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create target: %w", err)
	}
	for _, directory := range missing {
		if err := os.Chmod(directory, 0o755); err != nil {
			return fmt.Errorf("set target mode: %w", err)
		}
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return fmt.Errorf("set target mode: %w", err)
	}
	return nil
}

func hasOwnedPrefix(name string) bool {
	for _, prefix := range SkillOwnedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func validateSkill(skill skills.Skill) error {
	if skill.Name == "" || !hasOwnedPrefix(skill.Name) || filepath.Base(skill.Name) != skill.Name {
		return fmt.Errorf("invalid skill name")
	}
	if len(skill.Files) == 0 {
		return fmt.Errorf("has no files")
	}
	for relative := range skill.Files {
		clean := path.Clean(relative)
		if relative == "" || path.IsAbs(relative) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("invalid file path")
		}
	}
	return nil
}

func installSkill(target string, skill skills.Skill) (bool, error) {
	stage, err := os.MkdirTemp(target, SkillStagePrefix)
	if err != nil {
		return false, err
	}

	defer func() { _ = safedelete.RemoveAll(target, stage) }()

	files := make([]string, 0, len(skill.Files))
	for relative := range skill.Files {
		files = append(files, relative)
	}
	sort.Strings(files)
	for _, relative := range files {
		filename := filepath.Join(stage, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			return false, err
		}
		if err := os.Chmod(filepath.Dir(filename), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(filename, skill.Files[relative], 0o644); err != nil {
			return false, err
		}
		if err := os.Chmod(filename, 0o644); err != nil {
			return false, err
		}
	}
	if err := verifyRegularTree(stage); err != nil {
		return false, err
	}

	destination := filepath.Join(target, skill.Name)
	info, err := os.Lstat(destination)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if os.IsNotExist(err) {
		return false, os.Rename(stage, destination)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("destination is not a directory")
	}

	previous, err := os.MkdirTemp(target, SkillPreviousPrefix)
	if err != nil {
		return false, err
	}
	if err := safedelete.Remove(target, previous); err != nil {
		return false, err
	}
	if err := exchangeSkillDirectories(stage, destination); err != nil {
		return false, err
	}
	if err := os.Rename(stage, previous); err != nil {
		return false, err
	}
	if err := safedelete.RemoveAll(target, previous); err != nil {
		return false, err
	}
	return true, nil
}

func verifyRegularTree(root string) error {
	return filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("staged tree contains a symlink")
		}
		if entry.IsDir() {
			return os.Chmod(filename, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("staged tree contains a non-regular file")
		}
		return os.Chmod(filename, 0o644)
	})
}
