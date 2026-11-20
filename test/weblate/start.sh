#!/bin/sh
# Starts a fresh Weblate instance for the acceptance tests and prints WEBLATE_URL, WEBLATE_API_TOKEN of the user admin,
# WEBLATE_REVIEWER_API_TOKEN of the user reviewer, who is only in Weblate's default Groups, and WEBLATE_TEST_WORKSPACE,
# the UUID of a Workspace, and WEBLATE_IMAGE, which later `docker compose` commands need, as KEY=value lines, for
# `export $(test/weblate/start.sh)` or `test/weblate/start.sh >> "$GITHUB_ENV"`.
#
# WEBLATE_IMAGE overrides the image from the Dockerfile, WEBLATE_PORT the local port (default 8080).
set -eu
cd "$(dirname "$0")"

WEBLATE_IMAGE=${WEBLATE_IMAGE:-$(sed -n 's/^FROM //p' Dockerfile)}
export WEBLATE_IMAGE
url=http://127.0.0.1:${WEBLATE_PORT:-8080}
token=wlu_acceptancetesttoken0001
reviewer_token=wlu_acceptancetestreviewer0001
workspace=00000000-0000-4000-8000-000000000001

docker compose down -v --remove-orphans >&2
docker compose up -d >&2

echo "Waiting for $WEBLATE_IMAGE at $url" >&2
deadline=$(($(date +%s) + 300))
until curl -fsS -o /dev/null "$url/healthz/" 2>/dev/null; do
	if [ -z "$(docker compose ps -q --status running weblate)" ]; then
		docker compose logs --tail 40 weblate >&2
		echo "The Weblate container exited." >&2
		exit 1
	fi
	if [ "$(date +%s)" -ge "$deadline" ]; then
		docker compose logs --tail 40 weblate >&2
		echo "Weblate did not answer /healthz/ within 300 seconds." >&2
		exit 1
	fi
	sleep 1
done

# The image creates the user "admin"; WEBLATE_ADMIN_NAME is only its display name.
docker compose exec -T weblate weblate shell -c "
from django.apps import apps
from weblate.auth.models import User
Token = apps.get_model('authtoken', 'Token')
user = User.objects.get(username='admin')
Token.objects.filter(user=user).delete()
Token.objects.create(user=user, key='$token')
# Weblate's API cannot create the token of another user; the tests add this user to the Groups they need.
reviewer = User.objects.create(username='reviewer', full_name='Reviewer', email='reviewer@example.com')
# Weblate creates a token for every new user, and a user has at most one.
Token.objects.filter(user=reviewer).delete()
Token.objects.create(user=reviewer, key='$reviewer_token')
# Weblate's API cannot list or create Workspaces.
from weblate.workspaces.models import Workspace
Workspace.objects.create(id='$workspace', name='Acceptance tests')
" >&2
curl -fsS -o /dev/null -H "Authorization: Token $token" "$url/api/"

printf 'WEBLATE_URL=%s\nWEBLATE_API_TOKEN=%s\nWEBLATE_REVIEWER_API_TOKEN=%s\nWEBLATE_TEST_WORKSPACE=%s\nWEBLATE_IMAGE=%s\n' \
	"$url" "$token" "$reviewer_token" "$workspace" "$WEBLATE_IMAGE"
