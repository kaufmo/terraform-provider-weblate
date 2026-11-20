#!/bin/sh
# Builds the bare gettext repository http://git.test.example/demo.git, which the nginx git server serves over dumb HTTP.
set -eu
rm -rf /srv/git/demo.git /tmp/work
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
git add .
git -c user.name=test -c user.email=test@example.com commit -q -m init
git clone -q --bare /tmp/work /srv/git/demo.git
cd /srv/git/demo.git
git update-server-info
