-- Fail fast at first boot if the baked postgresql.conf wasn't actually used
-- (e.g., someone started the container without `-c config_file=...`). Spock
-- silently doesn't work if wal_level isn't 'logical' or the extension isn't
-- preloaded, which is a nightmare to debug later. Fail here instead.
DO $$
BEGIN
    IF current_setting('wal_level') <> 'logical' THEN
        RAISE EXCEPTION 'wal_level=% (expected logical). The Spock postgresql.conf was not loaded.', current_setting('wal_level');
    END IF;
    IF current_setting('shared_preload_libraries') NOT LIKE '%spock%' THEN
        RAISE EXCEPTION 'shared_preload_libraries=% (expected to include spock).', current_setting('shared_preload_libraries');
    END IF;
    IF current_setting('track_commit_timestamp') <> 'on' THEN
        RAISE EXCEPTION 'track_commit_timestamp=off. Spock LWW conflict resolution needs on.';
    END IF;
END
$$;
