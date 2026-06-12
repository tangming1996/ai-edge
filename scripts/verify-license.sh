#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
GO_LICENSES_BIN=${GO_LICENSES_BIN:-go-licenses}
MODULE_PATH=$(cd "${ROOT_DIR}" && go list -m -f '{{.Path}}')
ALLOWED_LICENSES=${ALLOWED_LICENSES:-Apache-2.0 BSD-2-Clause BSD-3-Clause ISC MIT CC0-1.0}

if ! command -v "${GO_LICENSES_BIN}" >/dev/null 2>&1; then
  echo "missing go-licenses binary: ${GO_LICENSES_BIN}" >&2
  exit 1
fi

if ! find "${ROOT_DIR}" -maxdepth 1 -type f \( -name 'LICENSE' -o -name 'LICENSE.*' -o -name 'COPYING' -o -name 'COPYING.*' \) | grep -q .; then
  echo "repository license file is missing at the project root." >&2
  echo "Add LICENSE (or COPYING) before enabling full license verification." >&2
  exit 1
fi

allowed_pattern=$(printf '%s\n' ${ALLOWED_LICENSES} | paste -sd'|' -)
report_file=$(mktemp)
trap 'rm -f "${report_file}"' EXIT

(
  cd "${ROOT_DIR}"
  "${GO_LICENSES_BIN}" report --include_tests ./...
) >"${report_file}"

local_unknown=$(awk -F, -v module="${MODULE_PATH}" '$1 ~ ("^" module) && $3 == "Unknown" {print $1}' "${report_file}")
if [[ -n "${local_unknown}" ]]; then
  echo "project packages still resolve to unknown license:" >&2
  echo "${local_unknown}" >&2
  echo "Ensure the repository root LICENSE file matches the intended project license." >&2
  exit 1
fi

disallowed=$(awk -F, -v module="${MODULE_PATH}" -v allowed="^("$(printf '%s' "${allowed_pattern}")")$" '
  $1 !~ ("^" module) && $3 !~ allowed {print $0}
' "${report_file}")

if [[ -n "${disallowed}" ]]; then
  echo "found disallowed or unknown third-party licenses:" >&2
  echo "${disallowed}" >&2
  echo "allowed licenses: ${ALLOWED_LICENSES}" >&2
  exit 1
fi

echo "license verification succeeded"
