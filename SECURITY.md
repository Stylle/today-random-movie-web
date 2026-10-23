# Security policy

## Supported version

Security fixes are made on the latest release line.

## Public deployment baseline

- Put the service behind an HTTPS reverse proxy. The Compose file intentionally binds to `127.0.0.1` by default.
- Set a long, random `SETUP_TOKEN`, finish initialization, and keep the value out of Git.
- Do not enable public playback unless visitors should receive the private FnOS playback URL.
- Only add the actual reverse proxy IP/CIDR to `TRUSTED_PROXIES`; forwarded headers from any other peer are ignored.
- `ALLOW_PUBLIC_FNOS=false` restricts the media server target to private/loopback addresses. Link-local, multicast and unspecified destinations stay blocked.
- `ALLOW_PRIVATE_WEBHOOKS=false` prevents Webhook SSRF into the private network. Enable it only for a trusted local notification service.
- Back up `/data/app.db` and `/data/secret.key` together. The key decrypts stored FnOS credentials.
- Do not publish `/data`, `.env`, logs or database backups.

The application sets a restrictive CSP, frame denial, MIME sniffing protection and same-origin referrer policy. Sessions are random, stored as hashes, sent in HttpOnly cookies and paired with CSRF tokens for modifying requests.

## Reporting

Please open a private security advisory in the GitHub repository. Do not include live credentials, database files or private server addresses in a public issue.
