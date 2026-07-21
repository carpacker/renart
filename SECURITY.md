# Security policy

Renart is in public alpha. Security fixes are applied to the latest alpha
release; older alpha versions are not supported once a newer release is
available.

## Report a vulnerability

Please do not open a public issue for a suspected vulnerability. Use
[GitHub's private vulnerability reporting](https://github.com/renart-data/renart/security/advisories/new)
and include the affected version, impact, reproduction steps, and any proposed
mitigation. You should receive an acknowledgement within seven days.

## Local trust boundary

Renart reads and writes the current Git workspace and can execute
user-authored pipeline code. The web server listens on loopback by default and
does not provide remote-user authentication. Do not expose it to an untrusted
network. A non-loopback bind requires the explicit `--unsafe-allow-remote`
flag and should only be used behind a trusted access layer.

Treat workspaces, pipeline code, dependencies, connection definitions, and
notebooks as executable input. Review unknown repositories before opening or
running them, and use least-privilege warehouse credentials.
