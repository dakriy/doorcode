#!/bin/sh
# Vendors the web UI's third-party pieces at pinned versions and checksums,
# then builds the stylesheet. Run it through `make ui` after changing a
# template or ui/tailwind.css, and commit the results:
#
#   internal/app/ui/static/htmx.min.js  htmx, downloaded and verified
#   internal/app/ui/static/app.css      built by the Tailwind standalone CLI
#
# The Tailwind CLI itself is cached under build/tools and never committed.
# To move to a new version, change the version and every checksum below,
# taking the checksums from the release's own published sums.
set -eu

TAILWIND_VERSION=v4.3.3
HTMX_VERSION=v2.0.10
HTMX_SHA256=71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de

UI=internal/app/ui
TOOLS=build/tools

case "$(uname -s)-$(uname -m)" in
Linux-x86_64)
	asset=tailwindcss-linux-x64
	sum=dc61b3ac6b8c9ca874c0cc4c57b2409791a64c5540404ca5f5367360babc313a
	;;
Linux-aarch64 | Linux-arm64)
	asset=tailwindcss-linux-arm64
	sum=55fd0b241214eff3de1e8ee4f22796662f2d2e7a49bcfca7477cfd0bac398195
	;;
Darwin-x86_64)
	asset=tailwindcss-macos-x64
	sum=7922e0953f2110c05976e3bf58f14e643d90427575e766b7d433f5f80cbee7e1
	;;
Darwin-arm64)
	asset=tailwindcss-macos-arm64
	sum=cdf646702987a743464dff4d9c60fd4480d1c1e73dd819a9a67f1078815dce9d
	;;
*)
	echo "vendor-ui: no pinned Tailwind CLI for $(uname -s)-$(uname -m)" >&2
	exit 1
	;;
esac

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

# fetch URL SHA256 DEST: download URL to DEST unless DEST already has the
# checksum; fail without touching DEST when the download does not match.
fetch() {
	if [ -f "$3" ] && [ "$(sha256 "$3")" = "$2" ]; then
		return
	fi
	tmp="$3.download"
	curl -fsSL -o "$tmp" "$1"
	got=$(sha256 "$tmp")
	if [ "$got" != "$2" ]; then
		rm -f "$tmp"
		echo "vendor-ui: checksum mismatch for $1: got $got, want $2" >&2
		exit 1
	fi
	mv "$tmp" "$3"
}

mkdir -p "$TOOLS" "$UI/static"
tailwind="$TOOLS/$asset-$TAILWIND_VERSION"
fetch "https://github.com/tailwindlabs/tailwindcss/releases/download/$TAILWIND_VERSION/$asset" "$sum" "$tailwind"
chmod +x "$tailwind"
fetch "https://github.com/bigskysoftware/htmx/releases/download/$HTMX_VERSION/htmx.min.js" "$HTMX_SHA256" "$UI/static/htmx.min.js"

"$tailwind" --input "$UI/tailwind.css" --output "$UI/static/app.css" --minify
echo "vendor-ui ok: Tailwind $TAILWIND_VERSION, htmx $HTMX_VERSION"
