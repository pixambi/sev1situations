## Briefing

Tell the trainee the customer's report, and nothing more:

> "Connections to the SSH target work once, then get cut off."

Do not reveal the cause. Pressing **Apply breakage** updates the target to allow one connection per session and
a session of 60 seconds. Nothing is wrong with the controllers, the worker or the network.

**What the trainee should see:**

- A second connection in the same session is closed at once. The `boundary connect` output shows
  `Connections Left: 0`.
- A connection that stays open is closed after about a minute, with `Reason: Session has expired`.
- `boundary targets read -id=<target id>` shows `Session Connection Limit: 1` and `Session Max Seconds: 60`.

You can run commands in a terminal on `node-1` from the Cluster tab. Inside a container Boundary listens on
`http://127.0.0.1:9200`, so start with `export BOUNDARY_ADDR=http://127.0.0.1:9200` and sign in with the admin
login and password from the Cluster tab. The target ID is there too.

Continue once the check passes: the target shows a connection limit of 1 and a session lifetime of 60 seconds.

## Hints

- Two different symptoms, two different settings. Which setting explains each?
- Read the target. What does it say about connections and about time?
- A session that is already open was authorised under the old values. How would you know the new ones apply?
- `-1` means no limit.

## Solution

The target was set to `session_connection_limit` 1 and `session_max_seconds` 60.

## References

- [Targets](https://developer.hashicorp.com/boundary/docs/concepts/domain-model/targets)
