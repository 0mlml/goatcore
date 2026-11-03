"""The reader-is-stale case.

A node reporting a serial NEWER than the reader's artifact is not an
unexplainable node — it is a node the READER cannot explain, because the
reader's copy is behind. Same grade, opposite first action, so the sentence
must not blame the node.
"""
from goat_consistency import grading


def _artifact(serial, content_hash, tail=()):
    return {
        "serial": serial,
        "content_hash": content_hash,
        "generated_at": "2026-08-10T00:00:00Z",
        "history": [
            {"serial": s, "content_hash": h, "generated_at": "2026-08-09T00:00:00Z"}
            for s, h in tail
        ],
    }


def test_node_ahead_of_reader_blames_the_reader_not_the_node():
    # Reader on 45, node stamped 113 by a converge.
    art = _artifact(45, "sha256:aaa", tail=[(44, "sha256:bbb"), (43, "sha256:ccc")])
    cell = grading.account_for(art, "sha256:zzz", observed_serial=113)

    assert cell["state"] == "unaccountable", "the grade is unchanged — no grammar edit"
    d = cell["detail"]
    assert "STALE" in d, "must say the reader is stale"
    assert "113" in d and "45" in d, "must name both serials"
    assert "do NOT" in d and "re-converge" in d, "must warn off the wrong action"


def test_without_the_serial_the_sentence_is_the_original_one():
    # Corpus-pinned behaviour: absent the new signal, nothing changes.
    art = _artifact(45, "sha256:aaa", tail=[(44, "sha256:bbb")])
    cell = grading.account_for(art, "sha256:zzz")
    assert cell["state"] == "unaccountable"
    assert cell["detail"].startswith("matches neither serial")
    assert "STALE" not in cell["detail"]


def test_node_behind_or_equal_is_untouched_by_the_new_branch():
    # A node genuinely behind must still read as behind, not as a stale reader.
    art = _artifact(45, "sha256:aaa", tail=[(44, "sha256:bbb")])
    cell = grading.account_for(art, "sha256:bbb", observed_serial=44)
    assert cell["state"] == "behind"

    # And an older-serial unaccountable value keeps the original sentence.
    cell = grading.account_for(art, "sha256:zzz", observed_serial=40)
    assert cell["state"] == "unaccountable"
    assert cell["detail"].startswith("matches neither serial")
