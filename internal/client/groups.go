package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Group is a Weblate Group with the fields of Weblate's Group serializer that the provider manages.
type Group struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// DefiningProject is the API URL of the Project that the Group is scoped to, or nil for an instance-wide Group.
	DefiningProject   *string `json:"defining_project"`
	ProjectSelection  int64   `json:"project_selection"`
	LanguageSelection int64   `json:"language_selection"`
	Enforced2FA       bool    `json:"enforced_2fa"`
	// The Grants, each as a list of API URLs.
	Roles          []string `json:"roles"`
	Languages      []string `json:"languages"`
	Projects       []string `json:"projects"`
	ComponentLists []string `json:"componentlists"`
	Components     []string `json:"components"`
}

// GrantType is one kind of Grant: the association endpoint below the Group and the body key that it takes. The keys
// differ from the ones in Weblate's OpenAPI document, which does not match the code.
type GrantType struct {
	Endpoint, Key string
}

// The Grant types. Roles, Projects, Component lists and Components take a numeric ID, Languages a language code.
var (
	RoleGrants          = GrantType{"roles", "role_id"}
	LanguageGrants      = GrantType{"languages", "language_code"}
	ProjectGrants       = GrantType{"projects", "project_id"}
	ComponentListGrants = GrantType{"componentlists", "component_list_id"}
	ComponentGrants     = GrantType{"components", "component_id"}
)

// Grants returns the API URLs of the Group's Grants of the given type.
func (g *Group) Grants(t GrantType) []string {
	switch t {
	case RoleGrants:
		return g.Roles
	case LanguageGrants:
		return g.Languages
	case ProjectGrants:
		return g.Projects
	case ComponentListGrants:
		return g.ComponentLists
	case ComponentGrants:
		return g.Components
	}
	return nil
}

// AddGrant grants the object with the given key to the Group with the given ID.
func (c *Client) AddGrant(ctx context.Context, group int64, t GrantType, key any) error {
	return c.Do(ctx, http.MethodPost, GroupRef(group)+t.Endpoint+"/", map[string]any{t.Key: key}, nil)
}

// RemoveGrant removes the Grant of the object with the given key from the Group with the given ID.
func (c *Client) RemoveGrant(ctx context.Context, group int64, t GrantType, key any) error {
	return c.Do(ctx, http.MethodDelete, GroupRef(group)+t.Endpoint+"/"+url.PathEscape(fmt.Sprint(key))+"/", nil, nil)
}

// ComponentList is a Weblate Component list.
type ComponentList struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
}

func (c *Client) GetComponentList(ctx context.Context, slug string) (*ComponentList, error) {
	var l ComponentList
	if err := c.Do(ctx, http.MethodGet, "component-lists/"+url.PathEscape(slug)+"/", nil, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func GroupRef(id int64) string {
	return "groups/" + strconv.FormatInt(id, 10) + "/"
}

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
