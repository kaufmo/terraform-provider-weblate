package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Addon is a Weblate Add-on on a Component or a Project.
type Addon struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Component is the API URL of the Component that holds the Add-on, or nil for an Add-on on a Project.
	Component *string `json:"component"`
	// Project is the API URL of the Project that holds the Add-on, or nil for an Add-on on a Component.
	Project       *string         `json:"project"`
	Configuration json.RawMessage `json:"configuration"`
}

// AddonRef returns the API path of the Add-on with the given ID.
func AddonRef(id int64) string {
	return "addons/" + strconv.FormatInt(id, 10) + "/"
}

// GetAddon reads the Add-on with the given ID.
func (c *Client) GetAddon(ctx context.Context, id int64) (*Addon, error) {
	return c.addonRequest(ctx, http.MethodGet, AddonRef(id), nil)
}

// CreateAddon installs an Add-on on the Component or Project at owner, a path from ComponentRef or ProjectRef.
func (c *Client) CreateAddon(ctx context.Context, owner string, fields map[string]any) (*Addon, error) {
	return c.addonRequest(ctx, http.MethodPost, owner+"addons/", fields)
}

// UpdateAddon changes the given fields of the Add-on with the given ID. Weblate replaces the whole configuration.
func (c *Client) UpdateAddon(ctx context.Context, id int64, fields map[string]any) (*Addon, error) {
	return c.addonRequest(ctx, http.MethodPatch, AddonRef(id), fields)
}

// DeleteAddon removes the Add-on with the given ID.
func (c *Client) DeleteAddon(ctx context.Context, id int64) error {
	return c.Do(ctx, http.MethodDelete, AddonRef(id), nil, nil)
}

// FindAddon returns the Add-on with the given name on the Component or Project at owner, a path from ComponentRef or
// ProjectRef, or nil when there is none. Weblate has no endpoint that lists the Add-ons of one Component or Project,
// so it searches all Add-ons that the token may manage.
func (c *Client) FindAddon(ctx context.Context, owner, name string) (*Addon, error) {
	addons, err := List[Addon](ctx, c, "addons/")
	if err != nil {
		return nil, err
	}
	for i, a := range addons {
		if a.Name == name && (isURLOf(a.Component, owner) || isURLOf(a.Project, owner)) {
			return &addons[i], nil
		}
	}
	return nil, nil
}

// isURLOf reports whether apiURL, an absolute URL from Weblate, points to ref, a path relative to the API root.
func isURLOf(apiURL *string, ref string) bool {
	if apiURL == nil {
		return false
	}
	u, err := url.Parse(*apiURL)
	return err == nil && strings.HasSuffix(u.EscapedPath(), "/api/"+ref)
}

// IsAddonAlreadyInstalled reports whether err is Weblate's 400 response to installing the Add-on name a second time
// on the same Component or Project.
func IsAddonAlreadyInstalled(err error, name string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest &&
		strings.Contains(apiErr.Body, strconv.Quote("Add-on already installed: "+name))
}

// ComponentSlugPathFromURL returns the slug path from the API URL of a Component, such as
// https://weblate.example/api/components/docs/guides%252Fapi/, which is docs/guides/api.
func ComponentSlugPathFromURL(componentURL string) (string, error) {
	u, err := url.Parse(componentURL)
	if err != nil {
		return "", err
	}
	segments := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	n := len(segments)
	if n < 3 || segments[n-3] != "components" {
		return "", fmt.Errorf("%q is not the API URL of a Component", componentURL)
	}
	project, err := url.PathUnescape(segments[n-2])
	if err != nil {
		return "", err
	}
	rest, err := url.PathUnescape(segments[n-1])
	if err == nil {
		rest, err = url.PathUnescape(rest)
	}
	return project + "/" + rest, err
}

func (c *Client) addonRequest(ctx context.Context, method, ref string, body any) (*Addon, error) {
	var a Addon
	if err := c.Do(ctx, method, ref, body, &a); err != nil {
		return nil, err
	}
	return &a, nil
}
