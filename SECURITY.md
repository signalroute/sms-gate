# Security Policy

## Supported Versions

| Version | Supported |
|---|---|
| v2.0.x (main branch) | ✅ Active support |
| v1.x | ❌ End of life — please upgrade |

Only the latest release on the `main` branch receives security patches. We recommend always running the most recent tagged release.

---

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Report vulnerabilities privately via [GitHub Security Advisories](https://github.com/signalroute/sms-gate/security/advisories/new). This keeps the details confidential until a patch is available and coordinated disclosure can be arranged.

### What to include

A useful report contains:

1. A clear description of the vulnerability and its potential impact.
2. Affected version(s) or commit range.
3. Step-by-step reproduction instructions (or a proof-of-concept if applicable).
4. Any suggested mitigations you have already identified.

---

## Response Timeline

| Milestone | Target |
|---|---|
| Initial acknowledgement | Within **48 hours** of receipt |
| Severity assessment | Within **5 business days** |
| Patch / mitigation available | Within **30 days** for critical, **90 days** for non-critical |
| Public disclosure | Coordinated with reporter after patch is released |

We follow responsible disclosure: we will not publicly disclose vulnerability details until a patch is released, unless the reporter and maintainers agree on an earlier date.

---

## Scope

The following are **in scope**:

- Authentication and authorisation flaws in the WebSocket tunnel protocol.
- Remote code execution via crafted AT responses or malformed PDUs.
- Privilege escalation in the systemd service unit.
- Secrets disclosure (e.g. token leakage through logs or metrics).
- Denial-of-service attacks that affect the availability of message delivery.

The following are **out of scope**:

- Vulnerabilities in the underlying Linux kernel, USB drivers, or modem firmware.
- Attacks that require physical access to the device.
- Issues in third-party dependencies that do not affect sms-gate when used as intended (please report those upstream).

---

## PGP Key

A PGP key for encrypted email submissions is available at:

```
https://github.com/signalroute.gpg
```

> **Placeholder**: The project maintainers will publish a dedicated security contact PGP key. Until then, use the GitHub Security Advisory channel above.

---

## Security Hardening Notes

The default systemd service unit (`deployments/go-sms-gate.service`) includes the following hardening options:

- `DynamicUser=yes` — runs under an ephemeral UID with no persistent home directory.
- `ProtectSystem=strict` — root filesystem is read-only; only `StateDirectory` is writable.
- `PrivateTmp=yes` — private `/tmp` namespace.
- `NoNewPrivileges=yes` — prevents privilege escalation via setuid binaries.
- `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX` — limits network socket families.

We encourage operators to review and further tighten these settings according to their threat model.
