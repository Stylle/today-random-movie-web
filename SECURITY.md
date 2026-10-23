# Security policy

## Supported version

Security fixes are made on the latest release line.

## Public deployment baseline

- Put the service behind an HTTPS reverse proxy. The Compose file intentionally binds to `127.0.0.1` by default.
- Set a long, random `SETUP_TOKEN`, finish initialization, and keep the value out of Git.
- Do not enable public playback unless visitors should receive the private FnOS playback URL.
- Private/loopback reverse-proxy peers are detected automatically. Public direct peers cannot make the application trust forged forwarding headers; an explicit `TRUSTED_PROXIES` override remains available for unusual networks.
- FnOS and Webhook targets are administrator-controlled and may be public or private. Link-local, multicast and unspecified destinations remain blocked, redirects are bounded and response sizes/timeouts are limited.
- Back up `/data/app.db` and `/data/secret.key` together. The key decrypts stored FnOS credentials.
- Do not publish `/data`, `.env`, logs or database backups.

The runtime uses a read-only root filesystem, drops all Linux capabilities and forbids privilege escalation. It starts as root for compatibility with root-owned NAS bind mounts, but can write only explicitly mounted writable paths. The application sets a restrictive CSP, frame denial, MIME sniffing protection and same-origin referrer policy. Sessions are random, stored as hashes, sent in HttpOnly cookies and paired with CSRF tokens for modifying requests.

## Reporting

Please open a private security advisory in the GitHub repository. Do not include live credentials, database files or private server addresses in a public issue.
