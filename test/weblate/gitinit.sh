#!/bin/sh
# Builds the bare repository http://git.test.example/demo.git with gettext and SubRip files, which the nginx git server serves over dumb HTTP,
# and a copy at http://git.test.example/private/demo.git behind HTTP basic auth.
set -eu
rm -rf /srv/git/demo.git /srv/git/private /tmp/work
git init -q -b main /tmp/work
cd /tmp/work
mkdir po
cat > po/messages.pot <<'EOF'
msgid ""
msgstr ""
"Content-Type: text/plain; charset=UTF-8\n"

msgid "Hello"
msgstr ""
EOF
cat > po/de.po <<'EOF'
msgid ""
msgstr ""
"Content-Type: text/plain; charset=UTF-8\n"
"Language: de\n"

msgid "Hello"
msgstr "Hallo"
EOF
# Weblate installs the Add-on weblate.flags.same_edit on its own for a Component in the srt format.
mkdir srt
printf '1\n00:00:01,000 --> 00:00:02,000\nHello\n' > srt/en.srt
printf '1\n00:00:01,000 --> 00:00:02,000\nHallo\n' > srt/de.srt
git add .
git -c user.name=test -c user.email=test@example.com commit -q -m init
for repo in /srv/git/demo.git /srv/git/private/demo.git; do
	git clone -q --bare /tmp/work "$repo"
	git -C "$repo" update-server-info
done
