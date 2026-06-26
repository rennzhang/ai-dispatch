# Security

Do not report secrets in public issues.

ai-dispatch shells out to local AI coding CLIs. Those CLIs may read files, edit files, or run commands depending on their own configuration and the flags used by ai-dispatch.

Before using ai-dispatch in automation:

- use a workspace intended for agent execution
- avoid mounting secrets that the provider does not need
- keep notification hooks free of prompt text, response text, secrets, full stderr, and private paths
- review provider CLI authentication and permission settings

Report sensitive issues privately to the repository owner.
