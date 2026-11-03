// Package domain holds small string constants shared across packages so the
// HTTP, service, and audit layers all refer to the same enum values. Keeping
// them typed-but-stringy means they still serialize cleanly to JSON and
// SQL CHECK constraints.
package domain

type OrgRole string

const (
	OrgRoleOwner  OrgRole = "owner"
	OrgRoleAdmin  OrgRole = "admin"
	OrgRoleMember OrgRole = "member"
)

func (r OrgRole) Valid() bool {
	switch r {
	case OrgRoleOwner, OrgRoleAdmin, OrgRoleMember:
		return true
	}
	return false
}

type ModuleStatus string

const (
	ModuleStatusUnpaired ModuleStatus = "unpaired"
	ModuleStatusPaired   ModuleStatus = "paired"
	ModuleStatusDegraded ModuleStatus = "degraded"
	ModuleStatusDisabled ModuleStatus = "disabled"
)

// KnownModuleSlug is a convenience constant list. New slugs are allowed —
// nothing enforces this set at the DB level — but having canonical names
// makes the launcher UI less of a free-for-all.
type ModuleSlug string

const (
	ModuleSlugStash  ModuleSlug = "stash"
	ModuleSlugEvents ModuleSlug = "events"
	ModuleSlugHR     ModuleSlug = "hr"
)

// BrandingAssetRole is the semantic slot a branding asset fills. Keep the
// strings short and stable — mated modules switch on them.
type BrandingAssetRole string

const (
	BrandingAssetLogoPrimary BrandingAssetRole = "logo-primary"
	BrandingAssetLogoDark    BrandingAssetRole = "logo-dark"
	BrandingAssetFavicon     BrandingAssetRole = "favicon"
	BrandingAssetOGImage     BrandingAssetRole = "og-image"
)

// Audit action names used by audit.Record callers.
const (
	ActionOrganizationUpdate = "organization.update"
	ActionModuleCreate       = "module.create"
	ActionModuleUpdate       = "module.update"
	ActionModuleDelete       = "module.delete"
	ActionPairingTokenMint   = "pairing.token.mint"
	ActionPairingTokenRedeem = "pairing.token.redeem"
	ActionBrandingUpdate     = "branding.update"
	ActionBrandingAssetAdd   = "branding.asset.add"
	ActionBrandingAssetRm    = "branding.asset.delete"
)
