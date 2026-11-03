#!/usr/bin/env python3
"""Check PR results only against their tested revision before a branch push."""
import json
import re
import subprocess
import sys


def check(prs, head):
    if not isinstance(prs, list) or len(prs) > 1:
        raise ValueError('expected zero or one open PR for this branch')
    if not prs:
        return 'No open PR; verify current-head CI after publication.'
    pr = prs[0]
    remote = pr.get('headRefOid') if isinstance(pr, dict) else None
    if not isinstance(remote, str) or not re.fullmatch(r'[0-9a-f]{40}', remote):
        raise ValueError('invalid PR head revision')
    if remote != head:
        result = subprocess.run(['git', 'merge-base', '--is-ancestor', remote, head],
                                capture_output=True, text=True)
        if result.returncode != 0:
            raise ValueError('PR head is not a verified ancestor of local HEAD; fetch and reconcile')
        return ('PR checks belong to ancestor ' + remote + '; local HEAD is unpushed. '
                'Old failures remain unresolved until current-head CI passes after publication.')
    rollup = pr.get('statusCheckRollup')
    if not isinstance(rollup, list) or not rollup:
        raise ValueError('current PR head has no status-check evidence')
    for row in rollup:
        if not isinstance(row, dict):
            raise ValueError('malformed status-check entry')
        if row.get('__typename') == 'CheckRun':
            status = row.get('status')
            if status == 'COMPLETED':
                if row.get('conclusion') not in {'SUCCESS', 'NEUTRAL', 'SKIPPED'}:
                    raise ValueError('current PR check failed: ' + str(row.get('name', '?')))
            elif status not in {'QUEUED', 'IN_PROGRESS', 'WAITING', 'PENDING', 'REQUESTED'}:
                raise ValueError('unknown check status')
        elif row.get('__typename') == 'StatusContext':
            if row.get('state') not in {'SUCCESS', 'PENDING', 'EXPECTED'}:
                raise ValueError('current PR status failed: ' + str(row.get('context', '?')))
        else:
            raise ValueError('unknown status-check type')
    return 'Current-head checks have no completed failures; pending checks still require follow-up.'


def main():
    try:
        print(check(json.load(sys.stdin), sys.argv[1]))
        return 0
    except (ValueError, IndexError, OSError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
