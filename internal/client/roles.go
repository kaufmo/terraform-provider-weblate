package client

import "context"

// Role is a Weblate Role: a named set of permissions assigned to a Group.
type Role struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// ListRoles reads every Role the API token can see. Without the role.view permission Weblate returns only the Roles
// of the token user's own Groups.
func (c *Client) ListRoles(ctx context.Context) ([]Role, error) {
	return List[Role](ctx, c, "roles/")
}
