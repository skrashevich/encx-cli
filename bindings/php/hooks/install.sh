#!/bin/sh
#
# Install the encx PHP bindings pre-commit hook.
#
# Usage: bash bindings/php/hooks/install.sh [--force]
#
#   --force  replace a pre-commit hook that this installer did not write

set -e

force=0
for arg in "$@"; do
	case "$arg" in
		--force)
			force=1
			;;
		-h | --help)
			echo "Usage: $0 [--force]"
			echo ""
			echo "Installs the encx PHP bindings pre-commit hook."
			echo "  --force  replace a pre-commit hook that this installer did not write"
			exit 0
			;;
		*)
			echo "install.sh: unknown argument: $arg" >&2
			echo "Usage: $0 [--force]" >&2
			exit 2
			;;
	esac
done

root=$(git rev-parse --show-toplevel)
source="$root/bindings/php/hooks/pre-commit"

if [ ! -f "$source" ]; then
	echo "install.sh: $source is missing." >&2
	exit 1
fi

# marker identifies a hook written by this installer, so that reinstalling can
# upgrade our own hook while still refusing to clobber somebody else's.
marker="encx PHP bindings: refuse a commit"

# Honour core.hooksPath when the repository or the user has redirected hooks;
# a relative path is taken from the top of the working tree, as git does.
hooks_path=$(git config --get core.hooksPath || true)
if [ -n "$hooks_path" ]; then
	case "$hooks_path" in
		/*) hooks_dir="$hooks_path" ;;
		*) hooks_dir="$root/$hooks_path" ;;
	esac
	echo "install.sh: core.hooksPath is set to '$hooks_path'."
else
	hooks_dir="$(git rev-parse --git-path hooks)"
	case "$hooks_dir" in
		/*) ;;
		*) hooks_dir="$root/$hooks_dir" ;;
	esac
fi

target="$hooks_dir/pre-commit"

if [ -e "$target" ]; then
	if cmp -s "$source" "$target"; then
		chmod +x "$target"
		echo "install.sh: hook already installed at $target (unchanged)."
		exit 0
	fi
	if ! grep -q "$marker" "$target" 2>/dev/null && [ "$force" -eq 0 ]; then
		echo "" >&2
		echo "install.sh: $target already exists and was not written by this installer." >&2
		echo "" >&2
		echo "  Leaving it alone so that your own hook keeps working. Either merge the" >&2
		echo "  contents of $source into it by hand," >&2
		echo "  or replace it with:" >&2
		echo "" >&2
		echo "    sh $0 --force" >&2
		echo "" >&2
		exit 1
	fi
fi

mkdir -p "$hooks_dir"
cp "$source" "$target"
chmod +x "$target"

echo "install.sh: installed the PHP bindings pre-commit hook at $target"
