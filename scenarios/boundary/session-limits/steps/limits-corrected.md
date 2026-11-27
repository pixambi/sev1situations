## Briefing

The trainee guides you through the repair. You type, they decide. Do not volunteer the method. When they say the
limits are fixed, press **Verify**.

The check passes when the target allows more than one connection per session, or no limit, and a session of more
than 60 seconds, and a new session can be authorised on it. Any sensible values are accepted.

## Hints

- Update the TCP target, not the host or the role.
- There is a flag for each setting. `boundary targets update tcp -h` lists them.
- Limits are copied onto a session when it is authorised, so connect again to prove it.

## Solution

1. Sign in as the admin on `node-1` and read the target to see the problem:

   ```sh
   boundary targets read -id=<target id>
   ```

2. Update it with realistic limits. `-1` for the connection limit means unlimited:

   ```sh
   boundary targets update tcp -id=<target id> -session-connection-limit=-1 -session-max-seconds=28800
   ```

3. Start a new session and confirm it shows the new limits. Sessions that existed before keep the old ones.

## References

- [Targets](https://developer.hashicorp.com/boundary/docs/concepts/domain-model/targets)
- [Sessions](https://developer.hashicorp.com/boundary/docs/concepts/domain-model/sessions)
