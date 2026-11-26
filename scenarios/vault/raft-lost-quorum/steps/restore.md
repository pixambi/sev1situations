## Briefing

An optional extension. The cluster works again on one node, which is not safe. Ask the trainee how to
get back to three nodes without repeating the mistake.

The right answer is **fresh** nodes. The old node-2 and node-3 are gone with their data, and anything
that remembered the old three-member configuration must not rejoin. On the Cluster tab, press **Revive**
on `node-2` and `node-3`: each is recreated from its configuration with an empty data folder and joins
the cluster. A new member joins as a non-voter and is promoted by autopilot after its stabilisation
time, so give it a little while.

Press **Verify** when both are back. The check passes when the peer list shows three servers, all voters.

## Hints

- Should the new nodes keep any of the old data? Why not?
- How does a new node join an existing Raft cluster?
- A node that has just joined is not a voter straight away. Which Vault feature promotes it?

## Solution

Revive `node-2` and `node-3` from the Cluster tab. Each one is a fresh node: it starts with an empty data
folder, joins the leader with `vault operator raft join`, is unsealed with the cluster's keys and becomes
a voter once autopilot has promoted it. `vault operator raft list-peers` then shows three voters.

## References

- [Autopilot](https://developer.hashicorp.com/vault/docs/concepts/integrated-storage/autopilot)
