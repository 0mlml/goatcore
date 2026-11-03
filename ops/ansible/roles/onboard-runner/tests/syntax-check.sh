#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
ansible-playbook -i 'localhost,' -c local playbook.yml --syntax-check
python3 test_catalog_staging.py
