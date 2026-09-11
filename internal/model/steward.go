package model

// PreferenceOverlay stores user-facing rename/group/pin/hide/pause that must
// survive upstream pack updates. Scoped by set + item identity.
type PreferenceOverlay struct {
	ID        string `json:"id"`
	SetID     string `json:"setId"`
	SourceID  string `json:"sourceId,omitempty"`
	ItemKey   string `json:"itemKey"` // protocol|entry identity|lang|account
	Name      string `json:"name,omitempty"`
	Group     string `json:"group,omitempty"`
	Logo      string `json:"logo,omitempty"`
	TVGID     string `json:"tvgId,omitempty"`
	Pinned    bool   `json:"pinned,omitempty"`
	Hidden    bool   `json:"hidden,omitempty"`
	Paused    bool   `json:"paused,omitempty"`
	UpdatedAt string `json:"updatedAt"`
}

// DeletedSource keeps a soft-deleted source for undo.
type DeletedSource struct {
	ID        string `json:"id"`
	Source    Source `json:"source"`
	DeletedAt string `json:"deletedAt"`
}

// PersonalCredential is the long-lived "one personal client entry" handle.
type PersonalCredential struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Formats    []string `json:"formats"`
	Drivers    []string `json:"drivers,omitempty"` // capability filter; empty = all known
	BindingIDs []string `json:"bindingIds"`       // one binding per aggregate / set
	Generation int      `json:"generation"`
	CreatedAt  string   `json:"createdAt"`
	UpdatedAt  string   `json:"updatedAt"`
}
