#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")"

if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "ansible-playbook is not installed or not on PATH." >&2
  exit 1
fi

ansible_python="$(head -n 1 "$(command -v ansible-playbook)" | sed 's/^#!//; s/ .*//')"

if [[ -z "${ansible_python}" || ! -x "${ansible_python}" ]]; then
  echo "Could not determine the Python interpreter used by ansible-playbook." >&2
  exit 1
fi

if ! "${ansible_python}" -m pip --version >/dev/null 2>&1; then
  "${ansible_python}" -m ensurepip --upgrade
fi

"${ansible_python}" -m pip install -r requirements.txt
ansible-galaxy collection install -r requirements.yml

# Verify the installed ansible-core matches the pinned 2.21 major. Warns
# loudly when a developer or CI image bumps ansible-core, since the playbooks
# are not validated against other majors.
ansible_version_major="$(ansible --version | head -n1 | grep -oE '[0-9]+\.[0-9]+' | head -n1)"
if [[ "${ansible_version_major}" != "2.21" ]]; then
  echo "ansible-core ${ansible_version_major} detected; expected 2.21.x." >&2
  exit 1
fi
