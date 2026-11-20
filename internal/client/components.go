package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// Component is a Weblate Component. The typed fields are those the client and the provider need for identity;
// Fields holds the raw JSON of every field of the response by name.
type Component struct {
	ID      int64  `json:"id"`
	Slug    string `json:"slug"`
	Project struct {
		Slug string `json:"slug"`
	} `json:"project"`
	// Category is the URL of the Category that holds the Component, or nil at the Project root.
	Category       *string `json:"category"`
	SourceLanguage struct {
		Code string `json:"code"`
	} `json:"source_language"`
	URL string `json:"url"`
	// TaskURL is the background task that processes the Component after a create or update, or "" when there is none.
	TaskURL string                     `json:"task_url"`
	Fields  map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON fills the typed fields and Fields.
func (c *Component) UnmarshalJSON(data []byte) error {
	type plain Component
	if err := json.Unmarshal(data, (*plain)(c)); err != nil {
		return err
	}
	return json.Unmarshal(data, &c.Fields)
}

// ComponentRef returns the API path of the Component with the given slug path, such as docs/guides/api. Weblate
// takes the part after the Project slug as one path segment that is URL-encoded twice, guides%252Fapi.
func ComponentRef(slugPath string) string {
	project, rest, _ := strings.Cut(slugPath, "/")
	return "components/" + url.PathEscape(project) + "/" + url.PathEscape(url.PathEscape(rest)) + "/"
}

// GetComponent reads the Component at ref, a path from ComponentRef or the URL of the Component from Weblate.
func (c *Client) GetComponent(ctx context.Context, ref string) (*Component, error) {
	return c.componentRequest(ctx, http.MethodGet, ref, nil)
}

// FindComponentByID searches the Components of the Project for the numeric ID, because Weblate has no endpoint that
// reads a Component by ID. It returns nil when the Project has no such Component or does not exist.
func (c *Client) FindComponentByID(ctx context.Context, project string, id int64) (*Component, error) {
	components, err := List[Component](ctx, c, ProjectRef(project)+"components/")
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for i := range components {
		if components[i].ID == id {
			return &components[i], nil
		}
	}
	return nil, nil
}

// CreateComponent creates a Component in the Project with the given slug from the given fields of Weblate's
// Component serializer.
func (c *Client) CreateComponent(ctx context.Context, project string, fields map[string]any) (*Component, error) {
	return c.componentRequest(ctx, http.MethodPost, ProjectRef(project)+"components/", fields)
}

// UpdateComponent changes the given fields of the Component at ref. It sends PATCH, never PUT, because a PUT without
// category moves the Component to the Project root.
func (c *Client) UpdateComponent(ctx context.Context, ref string, fields map[string]any) (*Component, error) {
	return c.componentRequest(ctx, http.MethodPatch, ref, fields)
}

// DeleteComponent starts the deletion of the Component at ref. Weblate deletes it in the background; see
// WaitForDeletion.
func (c *Client) DeleteComponent(ctx context.Context, ref string) error {
	return c.Do(ctx, http.MethodDelete, ref, nil, nil)
}

// ComponentSlugPath returns the slug path of the Component, such as docs/guides/api, reading its Categories.
func (c *Client) ComponentSlugPath(ctx context.Context, comp *Component) (string, error) {
	if comp.Category == nil {
		return comp.Project.Slug + "/" + comp.Slug, nil
	}
	cat, err := c.categoryRequest(ctx, http.MethodGet, *comp.Category, nil)
	if err != nil {
		return "", err
	}
	p, err := c.CategorySlugPath(ctx, cat)
	return p + "/" + comp.Slug, err
}

func (c *Client) componentRequest(ctx context.Context, method, ref string, body any) (*Component, error) {
	var comp Component
	if err := c.Do(ctx, method, ref, body, &comp); err != nil {
		return nil, err
	}
	return &comp, nil
}
