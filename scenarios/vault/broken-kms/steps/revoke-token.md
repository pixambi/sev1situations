## Briefing

Tell the trainee the customer's report, and nothing more:

> "After a maintenance window our Vault will not unseal."

Do not reveal the cause. Pressing **Apply breakage** revokes the token the cluster uses to reach its KMS,
then stops and starts Vault on all three nodes. The three nodes do not come back, because a Vault that
cannot use its seal exits at start-up. Be ready for the trainee to expect a sealed cluster: what they find is
that Vault is not running at all.

**What the trainee should see on any workload node:**

- `vault status` reports connection refused, because there is no Vault process.
- The end of `/vault/logs/operational.log` has an error about the seal configuration and a 403 from the KMS
  address, with `permission denied` or `invalid token`.
- The KMS Vault, `kms-node-1`, is healthy, and so is its transit key.

You can run commands in a terminal on any node from the Cluster tab. Inside a container Vault listens on
`http://127.0.0.1:8200`, so start with `export VAULT_ADDR=http://127.0.0.1:8200`. The KMS root token is on the
Cluster tab, under the KMS Vault.

Continue once the check passes: no workload node is running and the log shows the seal error.

## Hints

- Vault is not running, so there is no status to read. Where is Vault's own output?
- The error names an address and a permission problem. Whose address is it, and what does it refuse?
- What does a transit seal need from the Vault that holds the key?
- The key itself is still there. What is missing is the right to use it.

## Solution

The cluster's token was revoked on the KMS, so every node fails its seal check at start-up and exits. Nothing
is wrong with the cluster's data or with the key.

## References

- [Transit seal configuration](https://developer.hashicorp.com/vault/docs/configuration/seal/transit)
