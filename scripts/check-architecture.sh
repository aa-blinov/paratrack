#!/bin/sh
set -eu

module=$(sed -n 's/^module //p' go.mod | head -n 1)
if [ -z "$module" ]; then
	echo "architecture check: module path missing from go.mod" >&2
	exit 1
fi

# Read the package graph once. Re-running `go list` for every package makes
# this gate spend most of its time rebuilding the same metadata.
package_info=$(go list -f '{{.ImportPath}}|{{join .Imports " "}}|{{join .Deps " "}}' ./... | awk 'index($0, "/node_modules/") == 0')
package_paths=$(printf '%s\n' "$package_info" | cut -d'|' -f1)
package_imports() {
	printf '%s\n' "$package_info" | awk -F '|' -v package="$1" '$1 == package { print $2; exit }'
}
package_deps() {
	printf '%s\n' "$package_info" | awk -F '|' -v package="$1" '$1 == package { print $3; exit }'
}

historical_go_files=$(find cmd internal -type f -name '*.go' -print | awk '/\/[^/]*wave[0-9]+[^/]*\.go$/')
if [ -n "$historical_go_files" ]; then
	echo "architecture check: Go filenames must describe behavior, not implementation history:" >&2
	printf '%s\n' "$historical_go_files" >&2
	exit 1
fi
historical_e2e_files=$(find e2e -type f -name '*.py' -print | awk '/\/[^/]*wave[0-9]+[^/]*\.py$/')
if [ -n "$historical_e2e_files" ]; then
	echo "architecture check: E2E filenames must describe behavior, not implementation history:" >&2
	printf '%s\n' "$historical_e2e_files" >&2
	exit 1
fi
hardcoded_e2e_targets=$(grep -R -n -E --include='*.py' '^[[:space:]]*(BASE|BASE_URL)[[:space:]]*=[[:space:]]*"https?://' e2e || true)
if [ -n "$hardcoded_e2e_targets" ] ||
	! grep -Fq 'DEFAULT_BASE_URL = "http://127.0.0.1:8888"' e2e/target.py ||
	! grep -Fq 'os.environ.get("PARATRACK_BASE", DEFAULT_BASE_URL)' e2e/target.py; then
	echo "architecture check: E2E targets must use the shared local default and explicit PARATRACK_BASE override" >&2
	[ -z "$hardcoded_e2e_targets" ] || printf '%s\n' "$hardcoded_e2e_targets" >&2
	exit 1
fi
historical_test_names=$(grep -R -n -E --include='*_test.go' 'func Test[A-Za-z0-9_]*Wave[0-9]+' internal || true)
if [ -n "$historical_test_names" ]; then
	echo "architecture check: test names must describe behavior, not implementation history:" >&2
	printf '%s\n' "$historical_test_names" >&2
	exit 1
fi

unformatted=$(find cmd internal scripts/architecture -type f -name '*.go' -print0 | xargs -0 gofmt -l)
if [ -n "$unformatted" ]; then
	echo "architecture check: Go files must be gofmt-formatted:" >&2
	printf '%s\n' "$unformatted" >&2
	exit 1
fi

check_forbidden_imports() {
	package=$1
	shift
	dependencies=$(package_deps "$package")
	dependency_set=" $dependencies "
	for forbidden in "$@"; do
		case "$dependency_set" in
			*" $module/$forbidden "*)
			echo "architecture check: $package must not depend on $module/$forbidden" >&2
			exit 1
			;;
		esac
	done
}

check_no_direct_import() {
	package=$1
	forbidden=$2
	imports=$(package_imports "$package")
	case " $imports " in
		*" $module/$forbidden "*)
			echo "architecture check: $package must not directly import $module/$forbidden" >&2
			exit 1
			;;
	esac
}


architecture_dir="$(CDPATH= cd -- "$(dirname -- "$0")/architecture" && pwd)"
for check in \
	10-workflow-boundaries.sh \
	20-persistence-and-security.sh \
	30-transport.sh \
	40-package-roles.sh \
	50-workflow-invariants.sh; do
	. "$architecture_dir/$check"
done

echo "architecture check passed"
