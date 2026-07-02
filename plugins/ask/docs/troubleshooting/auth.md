# Auth errors — login failed, token invalid, 401 403

## AUTH ERROR: Not logged in

You haven't logged in yet, or the session for the active profile is missing.

```bash
bee auth profiles          # see which profiles exist and which is active
bee auth login             # log in to the active profile
```

If you're on the right profile but still see this, your `.bee_secret` file may have been deleted or moved — this invalidates the encrypted token:

```bash
bee auth logout            # clear the stale token
bee auth login             # re-login with a fresh token
```

## ERROR: 401 Unauthorized — wrong API token or expired token

Your API token is wrong, expired, or your CloudBees account lacks the required permission.

1. Log in to the CloudBees web UI and regenerate your API token under *Profile → Security → API Token*.
2. Re-login:
   ```bash
   bee auth logout
   bee auth login
   ```

## ERROR: 403 Forbidden — permission denied, access denied

403 means your account lacks permission on the active controller or requested resource.

```bash
bee controller current     # confirm you're on the right controller
bee controller list        # switch if needed
bee auth profiles          # confirm you're on the right profile
```

Your account may lack admin access to that resource (credential store, node management). Check with your CloudBees admin.

## `bee auth login` hangs at the token prompt

The hidden-input reader (`stty -echo`) requires a real terminal. In some restricted shells, it can stall. Pass the token as a flag to bypass:

```bash
bee auth login --url ... --username ... --token <token>
```
