# Weblate allows only https and ssh remotes on public hosts by default; the test git server is plain HTTP on the
# private compose network.
VCS_ALLOW_SCHEMES = {"https", "ssh", "http"}
VCS_RESTRICT_PRIVATE = False
# The dumb HTTP transport of the static git server cannot serve shallow clones.
VCS_CLONE_DEPTH = 0
