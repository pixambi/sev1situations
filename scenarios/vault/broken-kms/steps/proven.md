## Briefing

Fixing the token is not the same as fixing the cluster until a restart proves it. Ask the trainee how they would
know the repair holds, then press **Verify**.

The check stops and starts Vault on `node-3` and waits for it to come back unsealed, with nobody touching it. If
the new token is only in a shell, or only on some nodes, this is where it shows.

## Hints

- What does a node need on its disk to unseal itself after a restart?
- If the fix only worked while someone was logged in, would it survive a reboot?

## Solution

Nothing more to do if the token is in the configuration file of every node. The check restarts `node-3`, which
reads its own configuration, reaches the KMS with the new token, and unseals itself.

## References

- [Auto-unseal](https://developer.hashicorp.com/vault/docs/concepts/seal#auto-unseal)
