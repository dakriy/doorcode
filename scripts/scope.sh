# Sourced by the verify scripts. The single list of what is NOT application
# code; everything else in the module is application code and is measured by
# the coverage and mutation gates. Directories are module-relative.
NON_APP_DIRS="cmd internal/driver internal/testsupport internal/demo test"
# Connector code: implementations and production wiring.
CONNECTOR_DIRS="cmd internal/driver"

MODULE=$(go list -m)

# in_dirs DIRS: read import paths on stdin, print those inside any of DIRS.
in_dirs() {
	awk -v mod="$MODULE" -v dirs="$1" '
		BEGIN { n = split(dirs, d, " ") }
		{ for (i = 1; i <= n; i++) { p = mod "/" d[i]; if ($0 == p || index($0, p "/") == 1) { print; next } } }'
}

# not_in_dirs DIRS: read import paths on stdin, print those outside all DIRS.
not_in_dirs() {
	awk -v mod="$MODULE" -v dirs="$1" '
		BEGIN { n = split(dirs, d, " ") }
		{ for (i = 1; i <= n; i++) { p = mod "/" d[i]; if ($0 == p || index($0, p "/") == 1) next } print }'
}

app_pkgs() { go list ./... | not_in_dirs "$NON_APP_DIRS" | paste -sd, -; }
driver_pkgs() { go list ./... | in_dirs "internal/driver" | paste -sd, -; }

# Regexes for gremlins --exclude-files: every non-application file.
mutation_excludes() {
	for d in $NON_APP_DIRS; do printf -- '--exclude-files=^%s/\n' "$d"; done
	printf -- '--exclude-files=_test\\.go$\n'
}
