// Package edition is what this instance may do (v1.12): the free edition,
// the paid one for a family (pro) or the one for a team. A signed license
// file, checked offline, sets it; without one a new instance has a 30-day
// trial of the paid edition, then the free one. Nothing already kept is
// ever locked away: what an edition stops is making, setting up and
// running its features, never reading, deleting or turning them off.
package edition

import (
	"fmt"

	"kept/server/internal/apperr"
	"kept/server/internal/i18n"
)

type Edition int

const (
	Free Edition = iota
	Pro
	Team
)

func (e Edition) String() string {
	switch e {
	case Pro:
		return "pro"
	case Team:
		return "team"
	}
	return "free"
}

// Name is how people call it, in Chinese (translated where it is shown).
func (e Edition) Name() string {
	switch e {
	case Pro:
		return i18n.N("收费版")
	case Team:
		return i18n.N("企业版")
	}
	return i18n.N("免费版")
}

func (e Edition) AtLeast(o Edition) bool { return e >= o }

// Parse reads "free", "pro" or "team".
func Parse(s string) (Edition, error) {
	switch s {
	case "free":
		return Free, nil
	case "pro":
		return Pro, nil
	case "team":
		return Team, nil
	}
	return Free, fmt.Errorf("edition: unknown %q", s)
}

// Limits are how many people and shared workspaces fit; 0 is no limit.
type Limits struct {
	Members          int
	SharedWorkspaces int
}

// DefaultLimits are each edition's: the free one is a family of six with
// one shared workspace; the paid one fifteen people; a team's license says.
func DefaultLimits(e Edition) Limits {
	switch e {
	case Pro:
		return Limits{Members: 15}
	case Team:
		return Limits{}
	}
	return Limits{Members: 6, SharedWorkspaces: 1}
}

// Feature names the parts of the paid editions, for clients to show or
// hide their entries by.
type Feature string

const (
	FeatureAI          Feature = "ai"          // assistants, questions, similar documents
	FeaturePrivacy     Feature = "privacy"     // private documents, personal keys, emergency access
	FeatureScenes      Feature = "scenes"      // health and other scenes
	FeatureStorage     Feature = "storage"     // S3, WebDAV, tiering, encryption, backup and copies to storage
	FeatureMail        Feature = "mail"        // mail fetching, remote inbox
	FeatureUploadLinks Feature = "uploadLinks" // links for others to upload
	FeatureWorkflows   Feature = "workflows"   // workflows and webhooks
	FeatureSharing     Feature = "sharing"     // share bundles, collections, shared drawers, watermarks
	FeatureSSO         Feature = "sso"         // team: single sign-on
	FeatureQuota       Feature = "quota"       // team: quotas and upload allowances
	FeatureBranding    Feature = "branding"    // team: product name and logo
	FeaturePostgres    Feature = "postgres"    // team: moving to PostgreSQL (copy-to-postgres); running on it is open to all (v1.13.3)
)

var featureEdition = map[Feature]Edition{
	FeatureAI: Pro, FeaturePrivacy: Pro, FeatureScenes: Pro, FeatureStorage: Pro, FeatureMail: Pro,
	FeatureUploadLinks: Pro, FeatureWorkflows: Pro, FeatureSharing: Pro,
	FeatureSSO: Team, FeatureQuota: Team, FeatureBranding: Team, FeaturePostgres: Team,
}

// Features lists what e includes beyond the free edition, in a fixed order.
func Features(e Edition) []Feature {
	var out []Feature
	for _, f := range []Feature{FeatureAI, FeaturePrivacy, FeatureScenes, FeatureStorage, FeatureMail, FeatureUploadLinks,
		FeatureWorkflows, FeatureSharing, FeatureSSO, FeatureQuota, FeatureBranding, FeaturePostgres} {
		if e.AtLeast(featureEdition[f]) {
			out = append(out, f)
		}
	}
	return out
}

// EditionOf is the edition a feature belongs to.
func EditionOf(f Feature) Edition { return featureEdition[f] }

var (
	errNeedsPro  = apperr.WithCode(apperr.KindForbidden, "edition_required", i18n.N("这是收费版的功能，请管理员在“设置 → 实例管理 → 版本与授权”导入许可证"))
	errNeedsTeam = apperr.WithCode(apperr.KindForbidden, "edition_required", i18n.N("这是企业版的功能，请管理员在“设置 → 实例管理 → 版本与授权”导入许可证"))
	// errNotInBuild: the open-source build has no paid features at all (v1.13.1)
	errNotInBuild = apperr.WithCode(apperr.KindForbidden, "edition_required", i18n.N("这是开源的免费版，不包含收费功能"))
	// ErrOpenSource: licenses are for the official build (v1.13.1).
	ErrOpenSource = apperr.WithCode(apperr.KindInvalid, "edition_open_source", i18n.N("这是开源的免费版，不能导入许可证；收费功能请使用官方发布的版本"))
	// ErrMembers and ErrSharedWorkspaces: the limit is reached; nobody
	// already there is affected.
	ErrMembers          = apperr.WithCode(apperr.KindForbidden, "edition_limit", i18n.N("成员已经到了当前版本的上限，已有的成员不受影响；要再加人，请管理员升级版本"))
	ErrSharedWorkspaces = apperr.WithCode(apperr.KindForbidden, "edition_limit", i18n.N("共享空间已经到了当前版本的上限，已有的空间不受影响；要再建，请管理员升级版本"))
)

// Required is the error for something that needs edition e.
func Required(e Edition) error {
	if OpenSourceBuild {
		return errNotInBuild.With(map[string]any{"edition": e.String()})
	}
	if e >= Team {
		return errNeedsTeam.With(map[string]any{"edition": e.String()})
	}
	return errNeedsPro.With(map[string]any{"edition": e.String()})
}
