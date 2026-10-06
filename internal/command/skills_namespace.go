package command

import "github.com/aslanbrooke/jirahere/internal/layout"

const SkillDirPrefix = layout.Namespace + "-"

var SkillOwnedPrefixes = []string{SkillDirPrefix, "req-"}

const (
	SkillStagePrefix    = SkillDirPrefix + "stage-"
	SkillPreviousPrefix = SkillDirPrefix + "previous-"
)
