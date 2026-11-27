## Briefing

The trainee guides you through the repair. You type, they decide. Do not volunteer the method. When they say
jsmith can connect, press **Verify**.

The check passes when jsmith is allowed to authorize a session on the SSH target. It retries for a minute, as a
change to a role can take a moment to apply.

## Hints

- Add the missing action to a role that applies to jsmith, or make a new role for it.
- The grant needs the target, for example `ids=<target id>;actions=authorize-session`.
- The role's grant scopes must include the project that holds the target, and jsmith must be a principal.
- Check the fix as jsmith, not as admin, who is allowed anything.

## Solution

1. On `node-1`, sign in as the admin and find the role: `boundary roles list -scope-id=<org id>`.
2. Add the grant. Use the target ID from the Cluster tab:

   ```sh
   boundary roles add-grants -id=<role id> -grant='ids=<target id>;actions=authorize-session'
   ```

3. Sign in as jsmith and ask for a session: `boundary targets authorize-session -id=<target id>`. It now succeeds.

A new role works as well, as long as jsmith is a principal of it and the project is in its grant scopes.

## References

- [Roles](https://developer.hashicorp.com/boundary/docs/concepts/domain-model/roles)
- [Sessions](https://developer.hashicorp.com/boundary/docs/concepts/domain-model/sessions)
