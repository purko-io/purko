# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Purko, please report it responsibly.

**For now:** Open a [GitHub Issue](https://github.com/geored/purko/issues) with the `security` label. If the vulnerability is sensitive, please email the maintainers directly (contact information in the repository profile).

**What to include:**
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

## Supported Versions

| Version | Supported |
|---------|-----------|
| v1.5.x  | Yes       |
| < v1.5  | No        |

## Security Measures

Purko includes several built-in security features:

- **Graduated autonomy (Shu-Ha-Ri):** Agents start with restricted tool access and earn full autonomy through demonstrated reliability
- **Content filters:** Secrets and PII redaction in agent outputs
- **Per-agent RBAC:** Kubernetes-native ServiceAccount and Role per agent
- **Cost guardrails:** Token and cost limits per agent and workflow step

A comprehensive security policy will be published with the public release.
