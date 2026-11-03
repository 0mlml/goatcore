#!/bin/sh
# apply-view-patch.sh — inject the double-submit guard into the selfservice-ui's
# shared scripts partial (patches/0005-...).
#
# WHY. A login form submitted TWICE orphans the OAuth2 login challenge
# and delivers the operator to Ory's stock welcome page instead of the app they
# asked for, deterministically:
#
#   submit #1 -> 303 .../self-service/login/browser?aal=aal2&login_challenge=…
#   submit #2 -> 303 https://auth.<site>/                  <-- challenge orphaned
#
# The two submissions are the SAME form: identical action, identical csrf_token,
# identical content-length, ~800 ms apart. The browser never re-fetched the flow
# — that is what a double-click, an autofill re-fire, or an impatient second
# press produces. Kratos answers the second one on an already-consumed flow by
# redirecting to `default_browser_return_url`, which navigates the browser off
# the challenge-bearing step-up URL the FIRST submission had correctly returned.
# Everything downstream (the /welcome render, the whoami 403, the bare
# ?aal=aal2 redirect) follows from that navigation.
#
# This guard removes the trigger rather than mitigating the symptom. It is
# deliberately NOT a patch to maybeInitiate2FA: by the time that middleware
# fires the browser is already on /welcome and no login_challenge is in scope
# for it to preserve. That was recorded as the fix for several hours and was
# wrong; see F-341 "What is not fixed".
#
# SERIALIZATION SAFETY — the reason this does not simply disable the button.
# Kratos's login form carries `method=password` as the SUBMIT BUTTON's own
# name/value pair. A button disabled during the submit handler is excluded from
# form serialization, so the POST would lose `method` entirely and every login
# would fail with a validation error. The guard therefore blocks the second
# SUBMIT EVENT and defers any visual disabling to a macrotask, by which point
# the browser has already serialized the body.
#
# ONCE PER DOCUMENT. scripts.hbs is not included once per page: consent.hbs
# includes it and renders inside layouts/auth.hbs, which includes it again. The
# guard therefore latches on window and registers one listener however many
# times it is included. Without the latch the second copy cancelled the FIRST
# submit on the consent page and "Allow" sent nothing.
#
# Usage:
#   apply-view-patch.sh /usr/src/app/views/partials/scripts.hbs
set -eu

TARGET="${1:-/usr/src/app/views/partials/scripts.hbs}"
MARKER="goat-maintainer-double-submit-guard"

if [ ! -f "$TARGET" ]; then
  echo "apply-view-patch.sh: target not found: $TARGET" >&2
  exit 1
fi

if grep -q "$MARKER" "$TARGET" 2>/dev/null; then
  echo "apply-view-patch.sh: $MARKER already present — no-op"
  exit 0
fi

cat >> "$TARGET" <<'GUARD'

{{! goat-maintainer-double-submit-guard — see ops/ory-consent/patches/0005-*.patch }}
<script>
  (function () {
    // Install ONCE per document. This partial is not included once per page:
    // consent.hbs includes it AND renders inside layouts/auth.hbs, which
    // includes it again. Two copies of the listener below both see the same
    // submit event — the first marks the form, the second finds the mark and
    // cancels the FIRST submission. "Allow" then sends nothing at all.
    if (window.__dlfSubmitGuardInstalled) return;
    window.__dlfSubmitGuardInstalled = true;

    // One submission per rendered form. A second submit of the same flow is
    // answered by Kratos on a consumed flow and navigates the browser off an
    // in-flight OAuth2 login, orphaning the Hydra challenge.
    document.addEventListener(
      "submit",
      function (event) {
        var form = event.target;
        if (!form || form.tagName !== "FORM") return;

        if (form.hasAttribute("data-dlf-submitted")) {
          // Already sent. Swallow this one; the first response is still coming.
          event.preventDefault();
          event.stopImmediatePropagation();
          return;
        }
        form.setAttribute("data-dlf-submitted", "true");

        // Defer the visual disable to a macrotask. Disabling a submit button
        // synchronously drops its name/value (method=password) from the POST
        // body — the form would serialize without the method Kratos requires.
        window.setTimeout(function () {
          var controls = form.querySelectorAll(
            'button[type="submit"], input[type="submit"]'
          );
          for (var i = 0; i < controls.length; i++) {
            controls[i].setAttribute("aria-disabled", "true");
            controls[i].style.pointerEvents = "none";
            controls[i].style.opacity = "0.6";
          }
        }, 0);
      },
      true // capture: run before any framework handler can re-dispatch
    );
  })();
</script>
GUARD

echo "apply-view-patch.sh: injected $MARKER into $TARGET"
