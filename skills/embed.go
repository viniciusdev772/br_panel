// Package skills embeds the agent skills of this folder so `brpanel skills install` can write them
// without a checkout of the repository.
package skills

import "embed"

// FS holds every brpanel-* skill folder (SKILL.md, references/, scripts/).
//
//go:embed brpanel-*
var FS embed.FS
