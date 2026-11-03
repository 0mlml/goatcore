#!/bin/sh
# Install the harness's public key for the sudoer and run sshd in the foreground.
set -eu
install -d -m 0700 -o goat -g goat /home/goat/.ssh
if [ -f /pubkey ]; then
  install -m 0600 -o goat -g goat /pubkey /home/goat/.ssh/authorized_keys
fi
ssh-keygen -A >/dev/null 2>&1
# The docker socket is root:docker on the runner; let the sudoer's root use it (become: true).
exec /usr/sbin/sshd -D -e
