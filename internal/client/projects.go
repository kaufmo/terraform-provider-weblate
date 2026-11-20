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

// ProjectRef returns the API path of the Project with the given slug.
func ProjectRef(slug string) string {
	return "projects/" + url.PathEscape(slug) + "/"
}

// GetProject reads the Project with the given slug.
func (c *Client) GetProject(ctx context.Context, slug string) (*Project, error) {
	return c.projectRequest(ctx, http.MethodGet, ProjectRef(slug), nil)
}

// FindProjectByID searches the list of all Projects for the numeric ID, because Weblate has no endpoint that reads a
// Project by ID. It returns nil when no Project has the ID.
func (c *Client) FindProjectByID(ctx context.Context, id int64) (*Project, error) {
	projects, err := List[Project](ctx, c, "projects/")
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, nil
}

// CreateProject creates a Project with the given fields of Weblate's Project serializer.
func (c *Client) CreateProject(ctx context.Context, fields map[string]any) (*Project, error) {
	return c.projectRequest(ctx, http.MethodPost, "projects/", fields)
}

// UpdateProject changes the given fields of the Project with the given slug. It sends PATCH, never PUT, so that
// Weblate leaves every other field as it is.
func (c *Client) UpdateProject(ctx context.Context, slug string, fields map[string]any) (*Project, error) {
	return c.projectRequest(ctx, http.MethodPatch, ProjectRef(slug), fields)
}

// DeleteProject schedules the deletion of the Project with the given slug and returns the URL of the deletion task.
// The Project is gone only when the task has finished; see WaitForDeletion.
func (c *Client) DeleteProject(ctx context.Context, slug string) (taskURL string, err error) {
	var resp struct {
		TaskURL string `json:"task_url"`
	}
	err = c.Do(ctx, http.MethodDelete, ProjectRef(slug), nil, &resp)
	return resp.TaskURL, err
}

func (c *Client) projectRequest(ctx context.Context, method, ref string, body any) (*Project, error) {
	var p Project
	if err := c.Do(ctx, method, ref, body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
