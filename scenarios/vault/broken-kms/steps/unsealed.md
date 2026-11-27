## Briefing

The trainee guides you through the repair. You type, they decide. Do not volunteer the method. When they say
the cluster is back, press **Verify**.

The check passes when all three workload nodes are unsealed, the Raft peer list shows three voters, and the
secret at `secret/app` can be read. It waits for each node, so a node that was forgotten keeps it red.

## Hints

- A token is not enough on its own. What decides what it may do on the KMS?
- The policy needs only two operations on one path. Which?
- The token goes in the node's configuration file, in the `seal "transit"` stanza, not in your shell.
- Every node needs the new token, and Vault has to be started again on each one.
- Nothing needs unsealing by hand: with a transit seal a node opens itself once it starts.

## Solution

1. On the Cluster tab, open the KMS Vault's terminal (`kms-node-1`) and use its root token:

   ```sh
   export VAULT_ADDR=http://127.0.0.1:8200
   export VAULT_TOKEN=<the KMS root token from the Cluster tab>
   ```

2. The `autounseal` policy still exists on the KMS. Check it allows `update` on `transit/encrypt/autounseal` and
   `transit/decrypt/autounseal`: `vault policy read autounseal`.
3. Create a replacement token with it. Make it orphaned and periodic so it does not die with its parent or expire:

   ```sh
   vault token create -orphan -policy=autounseal -period=720h
   ```

4. On each workload node (`node-1`, `node-2`, `node-3`), put the new token in the seal stanza of
   `/etc/vault.d/vault.hcl`, for example with `sed -i 's/token .*/token      = "<new token>"/' /etc/vault.d/vault.hcl`.
5. Start Vault on each node again:

   ```sh
   vault server -config=/etc/vault.d/vault.hcl >> /vault/logs/operational.log 2>&1 &
   ```

6. Each node unseals itself. `vault status` shows `Sealed false`, and `vault operator raft list-peers` shows three voters.

## References

- [Transit seal configuration](https://developer.hashicorp.com/vault/docs/configuration/seal/transit)
- [Policies](https://developer.hashicorp.com/vault/docs/concepts/policies)
