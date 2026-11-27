## Briefing

Tell the trainee the customer's report, and nothing more:

> "jsmith can see the SSH target but cannot connect."

Do not reveal the cause. Pressing **Apply breakage** creates the user `jsmith`, who signs in with the
same password as the admin, which is on the Cluster tab, and a role `ssh-readonly` in the org scope whose grant scope is the project. The role gives
`jsmith` list and read on targets and nothing else, so the target is visible but a session is refused.

**What the trainee should see as jsmith:**

- Signing in works, so this is not an authentication problem.
- `boundary targets list -recursive` shows the target, so it is not a 404 either.
- `boundary targets authorize-session` fails with `PermissionDenied`, status 403.

You can run commands in a terminal on `node-1` from the Cluster tab. Inside a container Boundary listens on
`http://127.0.0.1:9200`, so start with `export BOUNDARY_ADDR=http://127.0.0.1:9200`. The admin login and password
and the IDs are on the Cluster tab.

Continue once the check passes: jsmith can list the target and is denied a session on it.

## Hints

- A 403 means Boundary knows who jsmith is and has decided against it. What decides?
- Read the roles that apply to jsmith, and their grants. What can jsmith do to a target, and what is missing?
- Which scope does the role apply to? A grant only counts where the role's grant scopes reach.
- Reading a target and connecting to it are different actions.

## Solution

`ssh-readonly` grants `list` and `read` and no `authorize-session`.

## References

- [Roles](https://developer.hashicorp.com/boundary/docs/concepts/domain-model/roles)
- [Permissions and grant strings](https://developer.hashicorp.com/boundary/docs/concepts/security/permissions)
