package client

import (
	"context"
	"net/http"
	"strconv"
)

// Group is a Weblate Group with the fields of Weblate's Group serializer that the provider manages. The Grants are
// not here yet.
type Group struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// DefiningProject is the API URL of the Project that the Group is scoped to, or nil for an instance-wide Group.
	DefiningProject   *string `json:"defining_project"`
	ProjectSelection  int64   `json:"project_selection"`
	LanguageSelection int64   `json:"language_selection"`
	Enforced2FA       bool    `json:"enforced_2fa"`
}

// GroupRef returns the API path of the Group with the given ID.
func GroupRef(id int64) string {
	return "groups/" + strconv.FormatInt(id, 10) + "/"
}

// GetGroup reads the Group with the given ID.
func (c *Client) GetGroup(ctx context.Context, id int64) (*Group, error) {
	return c.groupRequest(ctx, http.MethodGet, GroupRef(id), nil)
}

// CreateGroup creates a Group with the given fields of Weblate's Group serializer.
func (c *Client) CreateGroup(ctx context.Context, fields map[string]any) (*Group, error) {
	return c.groupRequest(ctx, http.MethodPost, "groups/", fields)
}

// UpdateGroup changes the given fields of the Group with the given ID. It sends PATCH, so that Weblate leaves every
// other field as it is.
func (c *Client) UpdateGroup(ctx context.Context, id int64, fields map[string]any) (*Group, error) {
	return c.groupRequest(ctx, http.MethodPatch, GroupRef(id), fields)
}

// DeleteGroup deletes the Group with the given ID.
func (c *Client) DeleteGroup(ctx context.Context, id int64) error {
	return c.Do(ctx, http.MethodDelete, GroupRef(id), nil, nil)
}

func (c *Client) groupRequest(ctx context.Context, method, ref string, body any) (*Group, error) {
	var g Group
	if err := c.Do(ctx, method, ref, body, &g); err != nil {
		return nil, err
	}
	return &g, nil
}
