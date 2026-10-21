#!/usr/bin/env bash
# Publishes the release GoReleaser built in dist/ to an HCP Terraform private registry.
# Usage: TFC_TOKEN=… TFC_ORGANIZATION=… GPG_KEY_ID=<key that signed SHA256SUMS> publish-hcp-registry.sh VERSION
# ponytail: not resumable; if a run fails after creating the version, delete it in HCP Terraform and re-run.
set -euo pipefail

version="${1#v}"
org="${TFC_ORGANIZATION:?}"
name=orchestra
dist="${DIST:-dist}"
api="${TFC_ADDRESS:-https://app.terraform.io}/api"
provider="$api/v2/organizations/$org/registry-providers/private/$org/$name"
sums="$dist/terraform-provider-${name}_${version}_SHA256SUMS"

call() { # METHOD URL [BODY]
  curl --silent --show-error --fail-with-body -X "$1" "$2" \
    -H "Authorization: Bearer ${TFC_TOKEN:?}" -H "Content-Type: application/vnd.api+json" ${3:+--data "$3"}
}

upload() { # FILE URL
  curl --silent --show-error --fail-with-body -T "$1" "$2"
}

if [ "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TFC_TOKEN" "$provider")" = 404 ]; then
  echo "Creating provider $org/$name"
  call POST "$api/v2/organizations/$org/registry-providers" "$(jq -n --arg org "$org" --arg name "$name" \
    '{data: {type: "registry-providers", attributes: {name: $name, namespace: $org, "registry-name": "private"}}}')" >/dev/null
fi

if ! call GET "$api/registry/private/v2/gpg-keys?filter%5Bnamespace%5D=$org" |
  jq -e --arg id "${GPG_KEY_ID:?}" 'any(.data[]; .attributes."key-id" | ascii_upcase == ($id | ascii_upcase))' >/dev/null; then
  echo "Registering GPG key $GPG_KEY_ID"
  call POST "$api/registry/private/v2/gpg-keys" "$(jq -n --arg org "$org" --arg armor "$(gpg --armor --export "$GPG_KEY_ID")" \
    '{data: {type: "gpg-keys", attributes: {namespace: $org, "ascii-armor": $armor}}}')" >/dev/null
fi

echo "Creating version $version"
links=$(call POST "$provider/versions" "$(jq -n --arg v "$version" --arg key "$GPG_KEY_ID" \
  --argjson protocols "$(jq -c .metadata.protocol_versions terraform-registry-manifest.json)" \
  '{data: {type: "registry-provider-versions", attributes: {version: $v, "key-id": $key, protocols: $protocols}}}')" | jq .data.links)
upload "$sums" "$(jq -r '."shasums-upload"' <<<"$links")"
upload "$sums.sig" "$(jq -r '."shasums-sig-upload"' <<<"$links")"

# SHA256SUMS lists every platform's zip (plus the manifest, which the private registry does not take).
while read -r shasum filename; do
  [[ $filename == *.zip ]] || continue
  platform=${filename#"terraform-provider-${name}_${version}_"}
  platform=${platform%.zip}
  os=${platform%%_*}
  arch=${platform#*_}
  echo "Uploading $os/$arch"
  url=$(call POST "$provider/versions/$version/platforms" "$(jq -n --arg os "$os" --arg arch "$arch" --arg sha "$shasum" --arg f "$filename" \
    '{data: {type: "registry-provider-version-platforms", attributes: {os: $os, arch: $arch, shasum: $sha, filename: $f}}}')" |
    jq -r '.data.links."provider-binary-upload"')
  upload "$dist/$filename" "$url"
done <"$sums"

echo "Published app.terraform.io/$org/$name $version"
