#!/usr/bin/env python3
"""Exercise actual guard SQL against populated, isolated PostgreSQL databases.

This verifies SQL migration and replica-trigger behavior, not Spock transport.
No host mounts, network, published ports or production credentials are used.

Default substrate is a throwaway postgres:16 container. With PG_BINDIR set to
a directory holding initdb/pg_ctl/psql, a temporary local cluster on a Unix
socket is used instead, and the container-only helper check is skipped.
"""

from pathlib import Path
import os
import shutil
import subprocess
import json
import tempfile
import sys
import time
import uuid


GUARDS = Path(__file__).resolve().parents[2] / "revocation-guard"
IMAGE = "postgres:16@sha256:ca0bd484cb98bf4b24eb1010e73fb3fcbd6714d240fbc1a10eea5b7dbecb641d"
IDENTITY = "00000000-0000-4000-8000-000000000001"
ACTIVE = "00000000-0000-4000-8000-000000000002"


def main():
    name = "revocation-rollout-" + uuid.uuid4().hex[:12]
    bindir = os.environ.get("PG_BINDIR")
    cluster = Path(tempfile.mkdtemp(prefix="rr-")) if bindir else None

    def docker(*args, **kwargs):
        return subprocess.run(
            ["docker", *args], text=True, capture_output=True, timeout=60, **kwargs
        )

    def psql(database, statement):
        if cluster:
            return subprocess.run(
                [str(Path(bindir) / "psql"), "-X", "-h", str(cluster), "-p", "5432", "-U", "postgres",
                 "-d", database, "-v", "ON_ERROR_STOP=1", "-tAXq"],
                input=statement, text=True, capture_output=True, timeout=60,
            )
        return docker(
            "exec", "-i", name, "psql", "-X", "-U", "postgres", "-d", database,
            "-v", "ON_ERROR_STOP=1", "-tAXq", input=statement,
        )

    def sql(database, statement, fail=False):
        result = psql(database, statement)
        if fail:
            assert result.returncode != 0, "incompatible schema unexpectedly accepted"
        else:
            assert result.returncode == 0, result.stdout + result.stderr
        return result.stdout.strip()

    def check(database, expression, message):
        assert sql(database, "SELECT " + expression + ";") == "t", message

    def start_container():
        result = docker(
            "run", "-d", "--rm", "--name", name, "--network", "none", "--read-only",
            "--tmpfs", "/var/lib/postgresql/data:rw,nosuid,nodev",
            "--tmpfs", "/var/run/postgresql:rw,nosuid,nodev", "--tmpfs", "/tmp:rw,nosuid,nodev",
            "-e", "POSTGRES_HOST_AUTH_METHOD=trust", IMAGE,
        )
        assert result.returncode == 0, result.stderr
        for _ in range(60):
            # TCP distinguishes the final server from entrypoint's temporary
            # Unix-socket-only initialization server.
            if docker("exec", name, "pg_isready", "-h", "127.0.0.1", "-U", "postgres").returncode == 0:
                return
            time.sleep(0.25)
        raise AssertionError("fixture PostgreSQL never became ready")

    guards = {kind: (GUARDS / (kind + ".sql")).read_text() for kind in ("netbird", "kratos")}
    try:
        if cluster:
            run = lambda *a: subprocess.run([str(Path(bindir) / a[0]), *a[1:]], text=True,
                                            capture_output=True, timeout=60)
            result = run("initdb", "-D", str(cluster / "data"), "-U", "postgres", "-A", "trust")
            assert result.returncode == 0, result.stderr
            result = run("pg_ctl", "-D", str(cluster / "data"), "-w", "-l", str(cluster / "log"), "-o",
                         f"-c listen_addresses='' -c unix_socket_directories={cluster} -p 5432", "start")
            assert result.returncode == 0, result.stdout + result.stderr
        else:
            start_container()

        for database in ("fixture_nb", "fixture_kr", "fixture_bad", "fixture_nb_peer", "fixture_kr_peer", "kratos"):
            sql("postgres", "CREATE DATABASE " + database + ";")
        sql("fixture_nb", """
            CREATE TABLE setup_keys(id text PRIMARY KEY, revoked boolean NOT NULL DEFAULT false);
            CREATE TABLE users(id text PRIMARY KEY, blocked boolean NOT NULL DEFAULT false);
            CREATE TABLE peers(id text PRIMARY KEY, user_id text, peer_status_login_expired boolean NOT NULL DEFAULT false);
            INSERT INTO setup_keys VALUES ('legacy-key',true),('active-key',false);
            INSERT INTO users VALUES ('legacy-user',true),('active-user',false);
            INSERT INTO peers VALUES ('legacy-peer','legacy-user',false),('active-peer','active-user',false);
        """)
        schema = """
            CREATE TABLE identities(id uuid PRIMARY KEY,state text NOT NULL,state_changed_at timestamptz);
            CREATE TABLE sessions(id text PRIMARY KEY,identity_id uuid);
        """
        sql("fixture_kr", schema + f"""
            INSERT INTO identities VALUES ('{IDENTITY}','inactive',now()),('{ACTIVE}','active',now());
            INSERT INTO sessions VALUES ('legacy-session','{IDENTITY}'),('active-session','{ACTIVE}');
        """)

        sql("fixture_nb", guards["netbird"])
        sql("fixture_kr", guards["kratos"])
        check("fixture_nb", "count(*)=2 FROM goat_revocations", "existing NetBird revocations not backfilled")
        check("fixture_kr", "count(*)=1 FROM goat_revocations", "existing inactive identity not backfilled")
        for database in ("fixture_nb", "fixture_kr"):
            check(database, "bool_and(at='-infinity'::timestamptz AND actor='guard-backfill') FROM goat_revocations",
                  "historical state must not outrank an explicit decision")
        check("fixture_kr", "count(*)=1 AND min(id)='active-session' FROM sessions", "session cleanup affected wrong identity")
        check("fixture_nb", "NOT peer_status_login_expired FROM peers WHERE id='active-peer'", "active bystander changed")
        print("PASS populated rollout: prior revocations captured, sessions cut, active rows retained", flush=True)

        for database, kind in (("fixture_nb", "netbird"), ("fixture_kr", "kratos")):
            sql(database, guards[kind])
        check("fixture_nb", "count(*)=2 FROM goat_revocations", "NetBird reinstall duplicated history")
        check("fixture_kr", "count(*)=1 FROM goat_revocations", "Kratos reinstall duplicated history")
        sql("fixture_nb", """
            SET session_replication_role=replica;
            UPDATE setup_keys SET revoked=false WHERE id='legacy-key';
            UPDATE users SET blocked=false WHERE id='legacy-user';
            UPDATE peers SET peer_status_login_expired=false WHERE id='legacy-peer';
            SET session_replication_role=origin;
        """)
        sql("fixture_kr", f"SET session_replication_role=replica; UPDATE identities SET state='active' WHERE id='{IDENTITY}';")
        check("fixture_nb", "revoked FROM setup_keys WHERE id='legacy-key'", "replica unrevoked historical key")
        check("fixture_nb", "blocked FROM users WHERE id='legacy-user'", "replica unblocked historical user")
        check("fixture_nb", "peer_status_login_expired FROM peers WHERE id='legacy-peer'", "replica restored blocked user's peer")
        check("fixture_kr", f"state='inactive' FROM identities WHERE id='{IDENTITY}'", "replica reactivated historical identity")
        print("PASS stale replica writes cannot restore pre-install access", flush=True)

        # Reinstall between the explicit decision and API update must not undo it.
        sql("fixture_nb", "SELECT goat_reinstate('netbird.user','legacy-user','fixture-operator');")
        sql("fixture_kr", f"SELECT goat_reinstate('kratos.identity','{IDENTITY}','fixture-operator');")
        sql("fixture_nb", guards["netbird"])
        sql("fixture_kr", guards["kratos"])
        # A disconnected site's baseline may arrive after that decision. It is
        # the same event (derived id, no site), delivered as the bootstrap
        # role delivers history, so the reinstatement already cancels it.
        for database, kind, subject in (("fixture_nb", "netbird.user", "legacy-user"),
                                        ("fixture_kr", "kratos.identity", IDENTITY)):
            baseline = json.dumps([{"id": sql(database, f"SELECT goat_revocation_baseline_id('{kind}','{subject}');"),
                                    "kind": kind, "subject_id": subject, "action": "revoke", "at": "-infinity",
                                    "actor": "guard-backfill", "site": None, "cancels": None}])
            assert sql(database, f"SELECT goat_revocations_merge('{baseline}'::jsonb);") == "0", \
                "another site's baseline is not the same event as this site's"
        sql("fixture_nb", "UPDATE users SET blocked=false WHERE id='legacy-user';")
        sql("fixture_kr", f"UPDATE identities SET state='active' WHERE id='{IDENTITY}';")
        check("fixture_nb", "NOT blocked FROM users WHERE id='legacy-user'", "baseline overrode explicit reinstatement")
        check("fixture_kr", f"state='active' FROM identities WHERE id='{IDENTITY}'", "baseline overrode identity reinstatement")
        print("PASS reinstall and late baseline preserve explicit reinstatement", flush=True)

        sql("fixture_nb", "UPDATE users SET blocked=true WHERE id='legacy-user'; UPDATE users SET blocked=false WHERE id='legacy-user';")
        sql("fixture_kr", f"UPDATE identities SET state='inactive' WHERE id='{IDENTITY}'; UPDATE identities SET state='active' WHERE id='{IDENTITY}';")
        check("fixture_nb", "blocked FROM users WHERE id='legacy-user'", "new revocation did not supersede reinstatement")
        check("fixture_kr", f"state='inactive' FROM identities WHERE id='{IDENTITY}'", "new identity revocation was lost")
        print("PASS fresh revocation still supersedes explicit reinstatement", flush=True)

        # ADR 1193 cancel sets: a reinstatement cancels the revoke events it
        # names, never "everything older by clock". Two sites are simulated in
        # one database by the events each would hold.
        sql("fixture_nb", "INSERT INTO users VALUES ('skew-user',false); INSERT INTO peers VALUES ('skew-peer','skew-user',false);")
        sql("fixture_nb", "UPDATE users SET blocked=true WHERE id='skew-user';")
        sql("fixture_nb", "SELECT goat_reinstate('netbird.user','skew-user','site-a-operator');")
        sql("fixture_nb", "UPDATE users SET blocked=false WHERE id='skew-user';")
        check("fixture_nb", "NOT blocked FROM users WHERE id='skew-user'", "normal reinstatement did not unblock")
        check("fixture_nb", """(SELECT cancels FROM goat_revocations WHERE subject_id='skew-user' AND action='reinstate')
                              = (SELECT array_agg(id) FROM goat_revocations WHERE subject_id='skew-user' AND action='revoke')""",
              "reinstatement did not name the revoke events visible at its site")
        # Site B, cut off and with its clock an hour behind, revoked the same
        # user; site A's reinstatement above could not have seen it. Its `at`
        # sorts BEFORE the reinstatement, as a skewed clock would stamp it.
        sql("fixture_nb", """INSERT INTO goat_revocations(kind,subject_id,action,at,actor,site)
                             SELECT 'netbird.user','skew-user','revoke',min(at) - interval '1 hour','site-b-operator','site-b'
                               FROM goat_revocations WHERE subject_id='skew-user' AND action='reinstate';""")
        check("fixture_nb", """(SELECT at FROM goat_revocations WHERE site='site-b' AND subject_id='skew-user')
                              < (SELECT at FROM goat_revocations WHERE action='reinstate' AND subject_id='skew-user')""",
              "fixture did not stamp the late revoke earlier than the reinstatement")
        check("fixture_nb", "goat_revoked('netbird.user','skew-user')", "an earlier-stamped revoke from another site was cancelled by clock")
        check("fixture_nb", "blocked FROM users WHERE id='skew-user'", "arriving revoke was not applied")
        check("fixture_nb", "peer_status_login_expired FROM peers WHERE id='skew-peer'", "arriving revoke left the user's peer logged in")
        sql("fixture_nb", "UPDATE users SET blocked=false WHERE id='skew-user';")
        check("fixture_nb", "blocked FROM users WHERE id='skew-user'", "unblock undid an uncancelled revoke")
        print("PASS a revoke the reinstating site never saw stands, whatever its timestamp", flush=True)
        # A second reinstatement, made after site B's revoke arrived, names it.
        sql("fixture_nb", "SELECT goat_reinstate('netbird.user','skew-user','site-a-operator');")
        sql("fixture_nb", "UPDATE users SET blocked=false WHERE id='skew-user';")
        check("fixture_nb", "NOT blocked FROM users WHERE id='skew-user'", "reinstatement naming every revoke did not unblock")
        # A revoke after that reinstatement re-revokes.
        sql("fixture_nb", "UPDATE users SET blocked=true WHERE id='skew-user';")
        sql("fixture_nb", "UPDATE users SET blocked=false WHERE id='skew-user';")
        check("fixture_nb", "blocked FROM users WHERE id='skew-user'", "revoke after reinstatement did not re-revoke")
        # The decision never reads `at`: re-stamping every event (as a copy to
        # a fresh table) leaves the answer unchanged. A reinstate row written
        # before cancel sets existed cancels nothing.
        check("fixture_nb", "count(*)=0 FROM goat_revocations WHERE action='reinstate' AND cancels IS NULL",
              "goat_reinstate wrote a reinstatement without a cancel set")
        sql("fixture_nb", "INSERT INTO goat_revocations(kind,subject_id,action,actor) VALUES ('netbird.user','skew-user','reinstate','pre-cancel-set');")
        check("fixture_nb", "goat_revoked('netbird.user','skew-user')", "a reinstatement without a cancel set cancelled a revoke")
        sql("fixture_nb", "INSERT INTO goat_revocations(kind,subject_id,action,actor,cancels) VALUES ('netbird.user','skew-user','revoke','bad','{}');", fail=True)
        print("PASS reinstate, re-revoke and legacy reinstate rows follow the cancel-set rule", flush=True)

        # A second populated site has independent history before membership.
        # No session_replication_role trick copies history: exercise the actual
        # importer with JSON snapshots as the Ansible tasks do.
        sql("fixture_nb_peer", """
            CREATE TABLE setup_keys(id text PRIMARY KEY, revoked boolean NOT NULL DEFAULT false);
            CREATE TABLE users(id text PRIMARY KEY, blocked boolean NOT NULL DEFAULT false);
            CREATE TABLE peers(id text PRIMARY KEY, user_id text, peer_status_login_expired boolean NOT NULL DEFAULT false);
            INSERT INTO users VALUES ('peer-user',true);
        """)
        sql("fixture_kr_peer", schema + f"INSERT INTO identities VALUES ('{ACTIVE}','inactive',now());")
        for local, peer, kind in (("fixture_nb","fixture_nb_peer","netbird"), ("fixture_kr","fixture_kr_peer","kratos")):
            sql(peer,guards[kind])
            def snapshot(db):
                return sql(db,"SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb) FROM goat_revocations r;")
            def merge(db, events, fail=False):
                return sql(db,"SELECT goat_revocations_merge('"+events.replace("'","''")+"'::jsonb);",fail=fail)
            original_peer = json.loads(snapshot(peer))
            original_local = json.loads(snapshot(local))
            merge(local,snapshot(peer))
            merge(peer,snapshot(local))
            union = snapshot(local)
            assert json.loads(union)==json.loads(snapshot(peer)), "history union differs"
            assert all(row in json.loads(union) for row in original_peer+original_local), "event metadata changed"
            assert merge(local,union)=="0" and merge(peer,union)=="0", "reimport not idempotent"
            # A reused ID with a different actor must roll back the entire
            # batch, including previously inserted events and their triggers.
            collision=json.loads(union)[0]
            collision['actor']='conflicting actor'
            fresh=dict(collision,id=str(uuid.uuid4()),actor='fixture-new-event')
            merge(local,json.dumps([fresh,collision]),fail=True)
            assert snapshot(local)==union,"collision partially changed history"
            merge(local,'{}',fail=True)
            merge(local,'[{"kind":"kratos.identity"}]',fail=True)
            for statement in ["UPDATE goat_revocations SET actor='overwrite'", "DELETE FROM goat_revocations"]:
                sql(local,"SET session_replication_role=replica; "+statement+";",fail=True)
            assert snapshot(local)==union,"replica mode rewrote append-only history"
        print("PASS two-site snapshots preserve event identity, union, idempotence and atomic collision refusal",flush=True)

        # Exercise the installed privileged helper, not a copied SQL snippet.
        if cluster:
            print("SKIP privileged helper: it runs psql through docker exec", flush=True)
        else:
            sql("kratos", schema + f"INSERT INTO identities VALUES ('{IDENTITY}','inactive',now());")
            sql("kratos", guards["kratos"])
            with tempfile.TemporaryDirectory() as tmp:
                config = Path(tmp)/"reinstatement.json"
                config.write_text(json.dumps({"postgres_container":name}))
                command = [sys.executable,str(GUARDS/"reinstate_identity.py"),"--config",str(config),
                           "--identity",IDENTITY,"--actor","fixture-operator","--reason","return approved; owner's request"]
                result = subprocess.run(command,text=True,capture_output=True,timeout=40)
                assert result.returncode==0,result.stdout+result.stderr
                assert json.loads(result.stdout)=={"id":IDENTITY,"actor":"fixture-operator","reinstated":True}
                check("kratos",f"state='inactive' FROM identities WHERE id='{IDENTITY}'","helper activated without console verification")
                check("kratos",f"NOT goat_revoked('kratos.identity','{IDENTITY}')","explicit reinstatement missing")
                sql("kratos",f"UPDATE identities SET state='active' WHERE id='{IDENTITY}';")
                check("kratos",f"state='active' FROM identities WHERE id='{IDENTITY}'","explicit recovery cannot activate")
                sql("kratos",f"SELECT goat_revoke('kratos.identity.deleted','{IDENTITY}','fixture-revoke');")
                before=sql("kratos","SELECT count(*) FROM goat_revocations;")
                result=subprocess.run(command,text=True,capture_output=True,timeout=40)
                assert result.returncode!=0,"deleted identity silently reinstated"
                assert sql("kratos","SELECT count(*) FROM goat_revocations;")==before,"refused recovery appended a decision"
            print("PASS privileged reinstatement is attributed, activation separate and deleted recovery refused",flush=True)

        sql("fixture_bad", f"""
            CREATE TABLE identities(id uuid PRIMARY KEY,state text NOT NULL,state_changed_at timestamptz);
            CREATE TABLE sessions(id text PRIMARY KEY);
            INSERT INTO identities VALUES ('{IDENTITY}','inactive',now());
        """)
        sql("fixture_bad", guards["kratos"], fail=True)
        check("fixture_bad", "to_regclass('public.goat_revocations') IS NULL", "failed installation left partial guard")
        print("PASS incompatible schema rolls back installation atomically", flush=True)
    finally:
        if cluster:
            subprocess.run([str(Path(bindir) / "pg_ctl"), "-D", str(cluster / "data"), "-m", "immediate", "stop"],
                           capture_output=True, timeout=60)
            shutil.rmtree(cluster, ignore_errors=True)
        else:
            docker("rm", "-f", name)


if __name__ == "__main__":
    main()
