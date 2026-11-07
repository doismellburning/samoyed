# Security

## Reporting a vulnerability

Please report a security problem privately, by email to <samoyed@doismellburning.co.uk>, rather than in a public issue.

Include what you found, how to reproduce it, and the version (from `samoyed-direwolf --version`) or commit you found it in.

Samoyed is maintained by volunteers, so there is no promised response time, but reports are read and acted on.

## Supported versions

Samoyed is pre-release.
Only the `main` branch is supported: fixes go there, and nothing is backported.

## Out of scope

These are not security problems, and can go to [GitHub issues](https://github.com/doismellburning/samoyed/issues) as usual:

* Crashes, hangs and panics, whatever triggers them
* AX.25 and APRS having no authentication - that's by design of the protocols
* Anyone on frequency being able to transmit, or to send frames Samoyed will decode
* The KISS and AGW network ports having no authentication - firewall them, or bind them to a trusted interface
