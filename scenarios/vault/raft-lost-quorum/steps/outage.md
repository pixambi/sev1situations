## Briefing

Tell the trainee the customer's report, and nothing more:

> "Vault is returning 500 on everything. Our applications cannot read a single secret."

Do not reveal the cause. When you press **Apply breakage**, two of the three nodes are removed, so the
cluster has lost quorum. The trainee should find that out from the evidence: let them ask for
`vault status`, the logs and the Raft peer list, and give them what a real command prints. You can run
the commands in a terminal on `node-1` from the Cluster tab. Inside the container Vault listens on
`http://127.0.0.1:8200`, so start with `export VAULT_ADDR=http://127.0.0.1:8200`.

**What the trainee should see on node-1:**

- `vault status` shows HA Mode `standby`, HA Cluster `n/a` and no Active Node Address.
- `/v1/sys/health` answers 429, and API calls answer 500 with `local node not active but active
  cluster node not found`.
- The log at `/vault/logs/operational.log`, or the Logs tab, repeats `Election timeout reached,
  restarting election`.

Continue once the check passes, which means node-1 is up and unsealed but knows of no leader.

## Hints

- How many voters does a three-node Raft cluster need to elect a leader? What is left?
- Is Vault broken, or is it waiting for votes that will never come? What does the log say?
- Can the other two nodes be brought back? (They are removed, with their data.)
- Raft has a documented way to tell the survivor who the members are. What is it called?

## Solution

The cluster lost two of three voters, so the one survivor can never win an election. It is not broken:
it needs to be told that it is the only member.

## References

- [Integrated storage](https://developer.hashicorp.com/vault/docs/concepts/integrated-storage)
