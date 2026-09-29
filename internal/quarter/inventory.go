package quarter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/layout"
	"github.com/aslanbrooke/jirahere/internal/profile"
	"github.com/aslanbrooke/jirahere/internal/safedelete"
	"github.com/aslanbrooke/jirahere/internal/settings"
)

type QuarterIssue = jira.QuarterIssue

type Searcher interface {
	SearchQuarterIssues(ctx context.Context, projectKey, quarterLabel string) ([]QuarterIssue, error)

	LatestQuarterUpdate(ctx context.Context, projectKey, quarterLabel string) (updated time.Time, found bool, err error)
}

var _ Searcher = (*jira.Client)(nil)

type ErrSettingUnset struct {
	Setting string
}

func (e *ErrSettingUnset) Error() string {
	return fmt.Sprintf("%s is not set; add it to settings.json", e.Setting)
}

const cacheTimeLayout = "2006-01-02T15:04:05.000Z07:00"

type cacheRecord struct {
	Key        string `json:"key"`
	Summary    string `json:"summary"`
	StatusID   string `json:"status_id"`
	StatusName string `json:"status_name"`
	Created    string `json:"created"`
	Updated    string `json:"updated"`
}

type cacheFile struct {
	Records    []cacheRecord `json:"records"`
	MaxCreated string        `json:"max_created"`
	MaxUpdated string        `json:"max_updated"`
	Project    string        `json:"project"`
	Quarter    string        `json:"quarter"`
}

func CacheDir(prof string) (string, error) {
	if prof != "" {
		if err := profile.Validate(prof); err != nil {
			return "", fmt.Errorf("invalid profile: %w", err)
		}
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("could not determine cache directory: %w", err)
	}
	if prof == "" {
		return filepath.Join(base, layout.Namespace), nil
	}
	return filepath.Join(base, layout.ProfilesDirName, prof), nil
}

func ClearCache(prof string) error {
	dir, err := CacheDir(prof)
	if err != nil {
		return err
	}
	return clearCache(dir)
}

func clearCache(dir string) error {
	parent := filepath.Dir(dir)

	if _, err := os.Stat(parent); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := safedelete.RemoveAll(parent, dir); err != nil {
		var outside *safedelete.ErrOutsideBase
		if errors.As(err, &outside) {
			return err
		}
		return fmt.Errorf("could not clear quarter inventory cache %s: %w", dir, err)
	}
	return nil
}

func Inventory(ctx context.Context, client Searcher, prof string) ([]QuarterIssue, error) {
	dir, err := CacheDir(prof)
	if err != nil {
		return nil, err
	}
	return inventory(ctx, client, dir, prof)
}

func InventoryForQuarter(ctx context.Context, client Searcher, quarterLabel, prof string) ([]QuarterIssue, error) {
	s, err := settings.Load(prof)
	if err != nil {
		return nil, fmt.Errorf("could not load settings: %w", err)
	}
	project := s.Defaults.Project
	if project == "" {
		return nil, &ErrSettingUnset{Setting: "defaults.project"}
	}

	dir, err := CacheDir(prof)
	if err != nil {
		return nil, err
	}
	return inventoryFor(ctx, client, dir, project, quarterLabel)
}

func inventory(ctx context.Context, client Searcher, cacheDir, prof string) ([]QuarterIssue, error) {
	s, err := settings.Load(prof)
	if err != nil {
		return nil, fmt.Errorf("could not load settings: %w", err)
	}
	project := s.Defaults.Project
	quarter := s.Defaults.CurrentQuarter
	if project == "" {
		return nil, &ErrSettingUnset{Setting: "defaults.project"}
	}
	if quarter == "" {
		return nil, &ErrSettingUnset{Setting: "defaults.current_quarter"}
	}

	return inventoryFor(ctx, client, cacheDir, project, quarter)
}

func inventoryFor(ctx context.Context, client Searcher, cacheDir, project, quarter string) ([]QuarterIssue, error) {
	path := filepath.Join(cacheDir, cacheFileName(project, quarter))
	if records, maxUpdated, ok := readCache(path, project, quarter); ok {

		probed, found, perr := client.LatestQuarterUpdate(ctx, project, quarter)
		if perr != nil {
			return nil, perr
		}
		if !found || !probed.After(maxUpdated) {
			return records, nil
		}

		if derr := removeCacheFile(cacheDir, path); derr != nil {
			return nil, derr
		}
	}

	records, err := client.SearchQuarterIssues(ctx, project, quarter)
	if err != nil {

		return nil, err
	}

	if err := writeCache(cacheDir, path, project, quarter, records); err != nil {
		return nil, err
	}
	return records, nil
}

func cacheFileName(project, quarter string) string {
	sum := sha256.Sum256([]byte(project + "\x00" + quarter))
	return fmt.Sprintf(
		"quarter-inventory-%s-%s-%s.json",
		sanitize(project), sanitize(quarter), hex.EncodeToString(sum[:8]),
	)
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.' || r == '_' || r == '-':
			return r
		default:
			return '_'
		}
	}, s)
}

func readCache(path, project, quarter string) (records []QuarterIssue, maxUpdated time.Time, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, false
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, time.Time{}, false
	}
	if cf.Project != project || cf.Quarter != quarter {
		return nil, time.Time{}, false
	}
	maxUpdated, err = time.Parse(cacheTimeLayout, cf.MaxUpdated)
	if err != nil {
		return nil, time.Time{}, false
	}
	out := make([]QuarterIssue, 0, len(cf.Records))
	for _, rec := range cf.Records {
		created, err := time.Parse(cacheTimeLayout, rec.Created)
		if err != nil {
			return nil, time.Time{}, false
		}
		updated, err := time.Parse(cacheTimeLayout, rec.Updated)
		if err != nil {
			return nil, time.Time{}, false
		}
		out = append(out, QuarterIssue{
			Key:     rec.Key,
			Summary: rec.Summary,
			Status: jira.Status{
				ID:   rec.StatusID,
				Name: rec.StatusName,
			},
			Created: created,
			Updated: updated,
		})
	}
	return out, maxUpdated, true
}

func removeCacheFile(cacheDir, path string) error {
	if err := safedelete.Remove(cacheDir, path); err != nil {
		var outside *safedelete.ErrOutsideBase
		if errors.As(err, &outside) {
			return err
		}
		return fmt.Errorf("could not remove stale quarter inventory cache %s: %w", path, err)
	}
	return nil
}

func writeCache(dir, path, project, quarter string, records []QuarterIssue) error {
	var maxCreated, maxUpdated time.Time
	recs := make([]cacheRecord, 0, len(records))
	for _, r := range records {
		if r.Created.After(maxCreated) {
			maxCreated = r.Created
		}
		if r.Updated.After(maxUpdated) {
			maxUpdated = r.Updated
		}
		recs = append(recs, cacheRecord{
			Key:        r.Key,
			Summary:    r.Summary,
			StatusID:   r.Status.ID,
			StatusName: r.Status.Name,
			Created:    r.Created.Format(cacheTimeLayout),
			Updated:    r.Updated.Format(cacheTimeLayout),
		})
	}

	doc := cacheFile{
		Records:    recs,
		MaxCreated: maxCreated.Format(cacheTimeLayout),
		MaxUpdated: maxUpdated.Format(cacheTimeLayout),
		Project:    project,
		Quarter:    quarter,
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("could not encode quarter inventory cache: %w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("could not create cache directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("could not set cache directory permissions %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "quarter-inventory-*.json.tmp")
	if err != nil {
		return fmt.Errorf("could not create temp cache file: %w", err)
	}
	tmpPath := tmp.Name()

	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("could not write temp cache file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("could not set cache file permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("could not write temp cache file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("could not save cache file: %w", err)
	}
	return nil
}
