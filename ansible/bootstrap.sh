#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")"

if ! command -v ansible-playbook >/dev/null 2>&1; then
  echo "ansible-playbook is not installed or not on PATH." >&2
  exit 1
fi

ansible_python="$(head -n 1 "$(command -v ansible-playbook)" | sed 's/^#!//')"

if [[ -z "${ansible_python}" || ! -x "${ansible_python}" ]]; then
  echo "Could not determine the Python interpreter used by ansible-playbook." >&2
  exit 1
fi

if ! "${ansible_python}" -m pip --version >/dev/null 2>&1; then
  "${ansible_python}" -m ensurepip --upgrade
fi

"${ansible_python}" -m pip install -r requirements.txt
ansible-galaxy collection install -r requirements.yml
