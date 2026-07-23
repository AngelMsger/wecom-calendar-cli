// Package wecomcalendarcli is the module root. It exists only to embed packaged
// assets — the companion `wecom-calendar` Skill — into the CLI binary, so that
// `wecom-calendar-cli skill install` can deploy a version-matched copy
// regardless of how the binary itself was installed (npm, go install, prebuilt,
// source).
package wecomcalendarcli

import "embed"

// SkillFS holds the companion Skill, rooted at "skills/wecom-calendar".
//
//go:embed all:skills/wecom-calendar
var SkillFS embed.FS

// SkillRoot is the path within SkillFS at which the Skill is rooted.
const SkillRoot = "skills/wecom-calendar"
