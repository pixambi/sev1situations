## Briefing

The trainee now guides you through the recovery. You type, they decide. Do not volunteer the method:
the point is that they know how the Raft recovery works. When they say it is done, press **Verify**.

The check passes when `node-1` is unsealed and active, the Raft peer list shows exactly one server that
is a voter and the leader, and a secret can be written (it writes `secret/recovery-check`).

## Hints

- `peers.json` is only read when Vault starts, after unseal. Placing it while Vault runs does nothing.
- It goes in the `raft` folder inside the storage path, `/vault/data/raft/`, not in `/vault/data/`.
- The `id` must match this node's `node_id` exactly. It is `node-1`.
- The address is this container's IP and the **cluster** port, 8201, not the API port 8200. The IP is on the Cluster tab.
- `non_voter` must be `false`.
- Vault has to be completely stopped before the file is placed, and unsealed again after it starts.

## Solution

1. In a terminal on `node-1`, stop Vault completely: `pkill vault`. Check with `pgrep vault` that it is gone.
2. Create the file, using this node's own address from the Cluster tab (shown here as `10.89.0.2`):

   ```sh
   cat > /vault/data/raft/peers.json <<'JSON'
   [
     { "id": "node-1", "address": "10.89.0.2:8201", "non_voter": false }
   ]
   JSON
   ```

3. Start Vault again the way it was started:

   ```sh
   vault server -config=/etc/vault.d/vault.hcl >> /vault/logs/operational.log 2>&1 &
   ```

4. Unseal with three of the five key shares (the Cluster tab shows them):
   `vault operator unseal <key>`, three times.
5. The log shows `raft recovery initiated`, then `election won` and `entering leader state`, and Vault
   deletes `peers.json` once it has read it.
6. Check: `vault status` shows HA Mode `active`, and `vault operator raft list-peers` lists one voter.
   Write a secret to prove the cluster takes writes.

## References

- [Manual recovery using peers.json](https://developer.hashicorp.com/vault/docs/concepts/integrated-storage#manual-recovery-using-peers-json)
