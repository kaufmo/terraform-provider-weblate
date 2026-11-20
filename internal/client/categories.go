package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// Category is a Weblate Category with the fields of Weblate's Category serializer that the provider reads. Weblate
// references the Project and the parent Category by their absolute API URLs.
type Category struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Project string `json:"project"`
	// Category is the URL of the parent Category, or nil at the Project root.
	Category *string `json:"category"`
	URL      string  `json:"url"`
}

// ProjectSlug returns the slug of the Category's Project, taken from the Project's URL.
func (c *Category) ProjectSlug() string {
	u, err := url.Parse(c.Project)
	if err != nil {
		return ""
	}
	return path.Base(u.Path)
}

// CategoryRef returns the API path of the Category with the given numeric ID.
func CategoryRef(id int64) string {
	return fmt.Sprintf("categories/%d/", id)
}

// GetCategory reads the Category with the given numeric ID.
func (c *Client) GetCategory(ctx context.Context, id int64) (*Category, error) {
	return c.categoryRequest(ctx, http.MethodGet, CategoryRef(id), nil)
}

// CreateCategory creates a Category with the given fields of Weblate's Category serializer.
func (c *Client) CreateCategory(ctx context.Context, fields map[string]any) (*Category, error) {
	return c.categoryRequest(ctx, http.MethodPost, "categories/", fields)
}

// UpdateCategory changes the given fields of the Category with the given numeric ID. It sends PATCH, never PUT,
// because a PUT without category moves the Category to the Project root.
func (c *Client) UpdateCategory(ctx context.Context, id int64, fields map[string]any) (*Category, error) {
	return c.categoryRequest(ctx, http.MethodPatch, CategoryRef(id), fields)
}

// DeleteCategory starts the deletion of the Category with the given numeric ID. Weblate deletes it in the background;
// see WaitForDeletion.
func (c *Client) DeleteCategory(ctx context.Context, id int64) error {
	return c.Do(ctx, http.MethodDelete, CategoryRef(id), nil, nil)
}

// CategorySlugPath returns the slug path of the Category, such as docs/guides/api, reading every parent Category.
func (c *Client) CategorySlugPath(ctx context.Context, cat *Category) (string, error) {
	return walkSlugPath(cat, func(ref string) (*Category, error) {
		return c.categoryRequest(ctx, http.MethodGet, ref, nil)
	})
}

// FindCategory returns the Category with the given slug path, or nil when the Project has no such Category or does
// not exist. Weblate has no endpoint that reads a Category by slug path, so it lists all Categories of the Project.
func (c *Client) FindCategory(ctx context.Context, slugPath string) (*Category, error) {
	project, _, _ := strings.Cut(slugPath, "/")
	categories, err := List[Category](ctx, c, ProjectRef(project)+"categories/")
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	byURL := map[string]*Category{}
	for i := range categories {
		byURL[categories[i].URL] = &categories[i]
	}
	inList := func(ref string) (*Category, error) {
		if parent, ok := byURL[ref]; ok {
			return parent, nil
		}
		return nil, fmt.Errorf("the parent Category %s is missing from the Categories of Project %s", ref, project)
	}
	for i := range categories {
		p, err := walkSlugPath(&categories[i], inList)
		if err != nil {
			return nil, err
		}
		if p == slugPath {
			return &categories[i], nil
		}
	}
	return nil, nil
}

func walkSlugPath(cat *Category, parent func(ref string) (*Category, error)) (string, error) {
	p := cat.Slug
	for ref := cat.Category; ref != nil; {
		next, err := parent(*ref)
		if err != nil {
			return "", err
		}
		p = next.Slug + "/" + p
		ref = next.Category
	}
	return cat.ProjectSlug() + "/" + p, nil
}

func (c *Client) categoryRequest(ctx context.Context, method, ref string, body any) (*Category, error) {
	var cat Category
	if err := c.Do(ctx, method, ref, body, &cat); err != nil {
		return nil, err
	}
	return &cat, nil
}
