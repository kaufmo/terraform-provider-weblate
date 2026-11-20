# PROTOTYPE: Weblate allows only https/ssh and public hosts by default; the test
# git server is plain HTTP on the private compose network.
VCS_ALLOW_SCHEMES = {"https", "ssh", "http"}
VCS_RESTRICT_PRIVATE = False
