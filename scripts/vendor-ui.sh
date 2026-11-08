#!/bin/sh
# Vendors the web UI's third-party pieces at pinned versions and checksums,
# then builds the stylesheet. Run it through `make ui` after changing a
# template or ui/tailwind.css, and commit the results:
#
#   internal/app/ui/static/htmx.min.js  htmx, downloaded and verified
#   internal/app/ui/static/swagger-ui/  Swagger UI's bundle, stylesheet, and
#                                       license, from the verified npm package
#   internal/app/ui/static/fullcalendar/ FullCalendar's standard bundle and
#                                       license, from the verified npm package
#   internal/app/ui/static/app.css      built by the Tailwind standalone CLI
#
# The Tailwind CLI and the npm packages are cached under build/tools and
# never committed. The Swagger UI and FullCalendar checksums are the SHA-256
# of the npm tarballs, whose published sha512 integrity they were checked
# against.
# To move to a new version, change the version and every checksum below,
# taking the checksums from the release's own published sums.
set -eu

TAILWIND_VERSION=v4.3.3
HTMX_VERSION=v2.0.10
HTMX_SHA256=71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de
SWAGGER_UI_VERSION=5.34.0
SWAGGER_UI_SHA256=15cdd55151558cf29ee70fb485332251a9911ebe27414b243ddd10fef9942394
FULLCALENDAR_VERSION=6.1.22
FULLCALENDAR_SHA256=44d3b853622df17902c2e1f34113ef40492a80e1ad2a6bd103683307a74c123d

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

swagger="$TOOLS/swagger-ui-dist-$SWAGGER_UI_VERSION.tgz"
fetch "https://registry.npmjs.org/swagger-ui-dist/-/swagger-ui-dist-$SWAGGER_UI_VERSION.tgz" "$SWAGGER_UI_SHA256" "$swagger"
mkdir -p "$UI/static/swagger-ui"
for f in swagger-ui-bundle.js swagger-ui.css LICENSE; do
	tar -xzOf "$swagger" "package/$f" >"$UI/static/swagger-ui/$f"
done

fullcalendar="$TOOLS/fullcalendar-$FULLCALENDAR_VERSION.tgz"
fetch "https://registry.npmjs.org/fullcalendar/-/fullcalendar-$FULLCALENDAR_VERSION.tgz" "$FULLCALENDAR_SHA256" "$fullcalendar"
mkdir -p "$UI/static/fullcalendar"
for f in index.global.min.js LICENSE.md; do
	tar -xzOf "$fullcalendar" "package/$f" >"$UI/static/fullcalendar/$f"
done

"$tailwind" --input "$UI/tailwind.css" --output "$UI/static/app.css" --minify
echo "vendor-ui ok: Tailwind $TAILWIND_VERSION, htmx $HTMX_VERSION, Swagger UI $SWAGGER_UI_VERSION, FullCalendar $FULLCALENDAR_VERSION"
