package model

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

func ID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

type Source struct {
	ProxyID              string   `json:"proxyId,omitempty"`
	HubProxyMode         string   `json:"hubProxyMode,omitempty"`
	ProbeIntervalMinutes int      `json:"probeIntervalMinutes"`
	NextProbe            string   `json:"nextProbe,omitempty"`
	SmokeKeyword         string   `json:"smokeKeyword,omitempty"`
	HubPluginID          string   `json:"hubPluginId,omitempty"`
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Protocol             string   `json:"protocol"`
	MediaTypes           []string `json:"mediaTypes"`
	Mode                 string   `json:"mode"`
	Capabilities         []string `json:"capabilities"`
	Trust                string   `json:"trust"`
	Network              string   `json:"network"`
	UpdatePolicy         string   `json:"updatePolicy"`
	IntervalMinutes      int      `json:"intervalMinutes"`
	URL                  string   `json:"url,omitempty"`
	RuntimeID            string   `json:"runtimeId,omitempty"`
	CatalogID            string   `json:"catalogId,omitempty"`
	Enabled              bool     `json:"enabled"`
	Health               string   `json:"health"`
	Score                int      `json:"score"`
	Failures             int      `json:"failures"`
	ActiveRevision       string   `json:"activeRevision,omitempty"`
	StagedRevision       string   `json:"stagedRevision,omitempty"`
	ETag                 string   `json:"etag,omitempty"`
	LastModified         string   `json:"lastModified,omitempty"`
	LastChecked          string   `json:"lastChecked,omitempty"`
	NextSync             string   `json:"nextSync,omitempty"`
	CreatedAt            string   `json:"createdAt"`
	UpdatedAt            string   `json:"updatedAt"`
}
type Endpoint struct {
	ID       string `json:"id"`
	SourceID string `json:"sourceId"`
	Role     string `json:"role"`
	URL      string `json:"url"`
}
type Secret struct {
	ID         string `json:"id"`
	OwnerID    string `json:"ownerId"`
	Ciphertext string `json:"ciphertext"`
	UpdatedAt  string `json:"updatedAt"`
}
type Normalized struct {
	Protocol        string          `json:"protocol"`
	MediaTypes      []string        `json:"mediaTypes"`
	Capabilities    []string        `json:"capabilities"`
	Items           []Item          `json:"items"`
	Config          json.RawMessage `json:"config,omitempty"`
	Warnings        []string        `json:"warnings"`
	RequiresRuntime bool            `json:"requiresRuntime"`
}
type Item struct {
	Rel      string          `json:"rel,omitempty"`
	MIME     string          `json:"mime,omitempty"`
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name"`
	URL      string          `json:"url,omitempty"`
	Group    string          `json:"group,omitempty"`
	Logo     string          `json:"logo,omitempty"`
	Language string          `json:"language,omitempty"`
	Region   string          `json:"region,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}
type Revision struct {
	ID         string     `json:"id"`
	SourceID   string     `json:"sourceId"`
	Hash       string     `json:"hash"`
	Normalized Normalized `json:"normalized"`
	Status     string     `json:"status"`
	Diff       Diff       `json:"diff"`
	CreatedAt  string     `json:"createdAt"`
}
type Diff struct {
	Added          int      `json:"added"`
	Removed        int      `json:"removed"`
	Changed        int      `json:"changed"`
	DomainChanges  []string `json:"domainChanges"`
	RequiresReview bool     `json:"requiresReview"`
}
type Catalog struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	Network         string `json:"network"`
	Trust           string `json:"trust"`
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"intervalMinutes"`
	NextSync        string `json:"nextSync,omitempty"`
	LastSync        string `json:"lastSync,omitempty"`
	ETag            string `json:"etag,omitempty"`
	LastModified    string `json:"lastModified,omitempty"`
}
type Candidate struct {
	ID           string `json:"id"`
	CatalogID    string `json:"catalogId"`
	Name         string `json:"name"`
	URL          string `json:"url"`
	Protocol     string `json:"protocol,omitempty"`
	Fingerprint  string `json:"fingerprint"`
	Status       string `json:"status"`
	SourceID     string `json:"sourceId,omitempty"`
	DiscoveredAt string `json:"discoveredAt"`
}
type Probe struct {
	ID        string   `json:"id"`
	SourceID  string   `json:"sourceId"`
	Level     string   `json:"level"`
	Success   bool     `json:"success"`
	LatencyMS int64    `json:"latencyMs"`
	Code      string   `json:"code"`
	Checks    []string `json:"checks"`
	CreatedAt string   `json:"createdAt"`
}
type Member struct {
	SourceID       string   `json:"sourceId"`
	Priority       int      `json:"priority"`
	Weight         int      `json:"weight"`
	Role           string   `json:"role"`
	MinScore       int      `json:"minScore"`
	MediaTypes     []string `json:"mediaTypes"`
	Languages      []string `json:"languages"`
	Regions        []string `json:"regions"`
	Devices        []string `json:"devices"`
	Networks       []string `json:"networks"`
	TimeoutMS      int      `json:"timeoutMs"`
	MaxConcurrency int      `json:"maxConcurrency"`
}
type ChannelRule struct {
	SourceID string `json:"sourceId,omitempty"`
	Match    string `json:"match"`
	Name     string `json:"name,omitempty"`
	Group    string `json:"group,omitempty"`
	Logo     string `json:"logo,omitempty"`
	TVGID    string `json:"tvgId,omitempty"`
	Hide     bool   `json:"hide"`
}
type SourceSet struct {
	NextPublish         string        `json:"nextPublish,omitempty"`
	ChannelRules        []ChannelRule `json:"channelRules,omitempty"`
	AutoPublish         bool          `json:"autoPublish"`
	MinAvailable        int           `json:"minAvailable"`
	MaxExcludedPercent  int           `json:"maxExcludedPercent"`
	PublishSignature    string        `json:"publishSignature,omitempty"`
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	Description         string        `json:"description"`
	Members             []Member      `json:"members"`
	CurrentPublication  string        `json:"currentPublication,omitempty"`
	PreviousPublication string        `json:"previousPublication,omitempty"`
	UpdatedAt           string        `json:"updatedAt"`
}
type Artifact struct {
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
	Hash        string `json:"hash"`
}
type Publication struct {
	ID              string              `json:"id"`
	SetID           string              `json:"setId"`
	Revision        string              `json:"revision"`
	Artifacts       map[string]Artifact `json:"artifacts"`
	SourceRevisions map[string]string   `json:"sourceRevisions"`
	Exclusions      map[string]string   `json:"exclusions"`
	CreatedAt       string              `json:"createdAt"`
}
type Binding struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	SetID      string   `json:"setId"`
	Hash       string   `json:"tokenHash,omitempty"`
	Formats    []string `json:"formats"`
	ExpiresAt  string   `json:"expiresAt"`
	Revoked    bool     `json:"revoked"`
	Generation int      `json:"generation"`
	CreatedAt  string   `json:"createdAt"`
}
type Runtime struct {
	UpdatedAt    string          `json:"updatedAt"`
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Driver       string          `json:"driver"`
	URL          string          `json:"url"`
	Network      string          `json:"network"`
	Trust        string          `json:"trust"`
	Capabilities []string        `json:"capabilities"`
	Health       string          `json:"health"`
	Version      string          `json:"version,omitempty"`
	LastChecked  string          `json:"lastChecked,omitempty"`
	LastSync     string          `json:"lastSync,omitempty"`
	State        json.RawMessage `json:"state,omitempty"`
}
type Job struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	TargetID   string `json:"targetId"`
	Status     string `json:"status"`
	Attempts   int    `json:"attempts"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"createdAt"`
	FinishedAt string `json:"finishedAt,omitempty"`
}
type Audit struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	TargetID  string `json:"targetId"`
	CreatedAt string `json:"createdAt"`
}

// Feedback accepts error classes only; never send content titles, URLs, or credentials.
type Feedback struct {
	ID            string `json:"id"`
	BindingID     string `json:"bindingId"`
	PublicationID string `json:"publicationId"`
	SourceID      string `json:"sourceId"`
	Code          string `json:"code"`
	CreatedAt     string `json:"createdAt"`
}
