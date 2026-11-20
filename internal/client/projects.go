package client

import (
	"context"
	"net/http"
	"net/url"
)

// Project is a Weblate Project with the fields of Weblate's Project serializer that the provider reads. The 12
// Inherited settings are not here yet.
type Project struct {
	ID                    int64   `json:"id"`
	Name                  string  `json:"name"`
	Slug                  string  `json:"slug"`
	Web                   string  `json:"web"`
	Instructions          string  `json:"instructions"`
	Workspace             *string `json:"workspace"`
	CheckFlags            string  `json:"check_flags"`
	TranslationReview     bool    `json:"translation_review"`
	SourceReview          bool    `json:"source_review"`
	CommitPolicy          int64   `json:"commit_policy"`
	EnableHooks           bool    `json:"enable_hooks"`
	LanguageAliases       string  `json:"language_aliases"`
	AccessControl         int64   `json:"access_control"`
	PublicSharing         bool    `json:"public_sharing"`
	UseSharedTM           bool    `json:"use_shared_tm"`
	ContributeSharedTM    bool    `json:"contribute_shared_tm"`
	UseWorkspaceTM        bool    `json:"use_workspace_tm"`
	ContributeWorkspaceTM bool    `json:"contribute_workspace_tm"`
	AutocleanTM           bool    `json:"autoclean_tm"`
	Enforced2FA           bool    `json:"enforced_2fa"`
}

// GetProject reads the Project with the given slug.
func (c *Client) GetProject(ctx context.Context, slug string) (*Project, error) {
	var p Project
	if err := c.Do(ctx, http.MethodGet, "projects/"+url.PathEscape(slug)+"/", nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
