#!/usr/bin/env python3
"""Run the actual reconciliation role against two isolated PostgreSQL sites."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[5]
GUARD = ROOT / 'ops/postgres/spock/revocation-guard/kratos.sql'
IMAGE = 'postgres:16@sha256:ca0bd484cb98bf4b24eb1010e73fb3fcbd6714d240fbc1a10eea5b7dbecb641d'


def main():
    sites = ['ledger-bootstrap-' + uuid.uuid4().hex[:10] for _ in range(2)]
    def docker(*args, **kwargs):
        return subprocess.run(['docker', *args], text=True, capture_output=True, timeout=60, **kwargs)
    def sql(site, statement):
        result = docker('exec', '-i', site, 'psql', '-X', '-U', 'postgres', '-d', 'kratos',
                        '-v', 'ON_ERROR_STOP=1', '-tAXq', input=statement)
        assert result.returncode == 0, result.stdout + result.stderr
        return result.stdout.strip()
    def snapshot(site):
        return sql(site, "SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM goat_revocations r;")
    try:
        for index, site in enumerate(sites):
            result = docker('run', '-d', '--rm', '--name', site, '--network', 'none', '--read-only',
                            '--tmpfs', '/var/lib/postgresql/data:rw,nosuid,nodev',
                            '--tmpfs', '/var/run/postgresql:rw,nosuid,nodev', '--tmpfs', '/tmp:rw,nosuid,nodev',
                            '-e', 'POSTGRES_HOST_AUTH_METHOD=trust', IMAGE)
            assert result.returncode == 0, result.stderr
            for _ in range(120):
                if docker('exec', site, 'pg_isready', '-h', '127.0.0.1', '-U', 'postgres').returncode == 0:
                    break
                time.sleep(.25)
            else:
                raise AssertionError('fixture did not become ready')
            result = docker('exec', site, 'psql', '-U', 'postgres', '-c', 'CREATE DATABASE kratos;')
            assert result.returncode == 0, result.stderr
            identity = f'00000000-0000-4000-8000-00000000000{index+1}'
            sql(site, f"""
                CREATE TABLE identities(id uuid PRIMARY KEY,state text NOT NULL,state_changed_at timestamptz);
                CREATE TABLE sessions(id text PRIMARY KEY,identity_id uuid);
                INSERT INTO identities VALUES ('{identity}','inactive',now());
            """)
            sql(site, GUARD.read_text())
        with tempfile.TemporaryDirectory() as directory:
            tmp = Path(directory)
            inventory = tmp / 'inventory.json'
            inventory.write_text(json.dumps({'all': {'hosts': {
                'fixture_local': {'ansible_connection': 'local'},
                'fixture_peer': {'ansible_connection': 'local'},
            }}}))
            play = tmp / 'play.json'
            play.write_text(json.dumps([{'hosts': 'fixture_local', 'gather_facts': False,
                'vars': {'revocation_database': 'kratos', 'revocation_local_container': sites[0],
                         'revocation_peer_host': 'fixture_peer', 'revocation_peer_container': sites[1]},
                'roles': ['revocation-ledger-bootstrap']}]))
            config = tmp / 'ansible.cfg'
            config.write_text(f'[defaults]\nroles_path={ROOT}/ops/ansible/roles\nlocal_tmp={tmp}/local\nremote_tmp={tmp}/remote\nretry_files_enabled=False\n')
            env = {key: os.environ[key] for key in ('PATH', 'HOME', 'TMPDIR') if key in os.environ}
            env.update(ANSIBLE_CONFIG=str(config), ANSIBLE_NOCOLOR='1')
            def run():
                return subprocess.run(['ansible-playbook', '-i', str(inventory), str(play)],
                                      env=env, text=True, capture_output=True, timeout=120)
            result = run()
            assert result.returncode == 0, result.stdout + result.stderr
            original = snapshot(sites[0])
            assert json.loads(original) == json.loads(snapshot(sites[1])), 'actual role failed to reconcile both sites'
            assert len(json.loads(original)) == 2, 'historical event lost or duplicated'
            result = run()
            assert result.returncode == 0, result.stdout + result.stderr
            assert snapshot(sites[0]) == original == snapshot(sites[1]), 'second role pass rewrote history'
            print('PASS actual Ansible role imports both histories and is idempotent', flush=True)
            collision = str(uuid.uuid4())
            for index, site in enumerate(sites):
                sql(site, f"INSERT INTO goat_revocations(id,kind,subject_id,action,actor) VALUES ('{collision}','kratos.identity','00000000-0000-4000-8000-000000000003','revoke','fixture-{index}');")
            before = [snapshot(site) for site in sites]
            result = run()
            assert result.returncode != 0, 'actual role accepted contradictory history'
            assert [snapshot(site) for site in sites] == before, 'failed role rewrote history'
            print('PASS actual Ansible role refuses a reused event ID without rewriting history', flush=True)
    finally:
        for site in sites:
            docker('rm', '-f', site)


if __name__ == '__main__':
    main()
