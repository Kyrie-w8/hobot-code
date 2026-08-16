# OpenExplorer LLM external package integration

This integration advertises a user-supplied OpenExplorer LLM board runtime only after agentd has
validated the configured root, runtime version header, AArch64 shared library, and representative
sample executables.

When the same package contains `.skillshare/skills/` and `docs/03_SKILLS_CATALOG.md`, Hobot Code
also validates and imports the cataloged official Skills into each Agent's private Pi settings
snapshot.

Set the runtime root in `~/.config/hobot-code/hobot.env`:

```sh
HOBOT_CODE_OPENEXPLORER_LLM_ROOT=/root/ssd/OELLM_Runtime/OpenExplorer_LLM_2.0.4
```

The package directory remains the source of truth; Hobot Code does not copy or modify its Skills.
An extra directory that is absent from the official customer catalog is inventoried but disabled.
The external package is not redistributed by Hobot Code and remains subject to its own license and
delivery terms.

Source metadata is read from the user-supplied package:

- Runtime version: `oellm_runtime/include/oellm_runtime_basic/oellm_runtime_version.h`
- Official Skill source: `.skillshare/skills/<name>/SKILL.md`
- Customer catalog: `docs/03_SKILLS_CATALOG.md`

Host-side OpenExplorer workflows must not run on the ARM64 S600. Hobot Code asks the user to select
an SSH target with `openexplorer_build_host`, verifies `x86_64` and optional CUDA support, and runs
each separately approved host command through `openexplorer_remote_run`. SSH keys and host aliases
remain under the RDK user's normal OpenSSH configuration.

Selecting a host does not copy the external package, source repositories, models, or calibration
data. Host-side Skills must ask the user for the remote OpenExplorer working tree, model, and output
paths before executing, and any referenced Skill script must already be accessible on that host.
