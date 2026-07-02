# Connection errors — server unreachable, certificate, TLS

## ERROR: Connection refused / cannot reach server

The CloudBees server URL is unreachable — host is down, port is wrong, or a firewall/VPN is blocking the connection.

1. Confirm the URL and port:
   ```bash
   bee auth profiles          # shows the server URL for each profile
   ```
2. Check the server is reachable (VPN up, host resolves):
   ```bash
   curl -sS -o /dev/null -w '%{http_code}\n' <server-url>
   ```
3. If the URL is wrong, re-login with the correct one:
   ```bash
   bee auth login --url <correct-url> --username ... --token <token>
   ```

## ERROR: Certificate / TLS verification failed — self-signed cert

The server presents a TLS certificate that can't be verified — typically a self-signed or internal CA certificate not trusted by this machine.

The fix is to trust the CA, not to disable verification. Add your organisation's CA certificate to the system trust store:

```bash
# Debian/Ubuntu
sudo cp your-ca.crt /usr/local/share/ca-certificates/
sudo update-ca-certificates
```

After trusting the CA, confirm the server URL uses `https://` and the certificate hostname matches the URL.

## ERROR: No active controller selected

`job`, `node`, and `cred` commands scope to a specific controller. You need to select one first:

```bash
bee controller list
bee controller select <name>
```

## ERROR: Controller not found

The controller name doesn't match — names are case-sensitive.

```bash
bee controller list        # confirm the exact name
bee controller select <exact-name>
```

## ERROR: 404 Not Found on job list / node list

Usually means the controller URL is wrong (missing `/` suffix, wrong port) or the endpoint isn't enabled on this controller type.

```bash
bee controller select <name>           # re-select to re-resolve the URL
bee controller info <name>             # check the resolved URL and capabilities
```
