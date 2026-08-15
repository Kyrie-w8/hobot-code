# OpenExplorer LLM runtime integration

This integration advertises a user-supplied OpenExplorer LLM board runtime only after agentd has
validated the configured root, runtime version header, AArch64 shared library, and representative
sample executables.

Set the runtime root in `~/.config/hobot-code/hobot.env`:

```sh
HOBOT_CODE_OPENEXPLORER_LLM_ROOT=/root/ssd/OELLM_Runtime/OpenExplorer_LLM_2.0.4
```

Install `SKILL.md` under `~/.agents/skills/openexplorer-llm-runtime/` on the S600 to make the
runtime workflow discoverable to the Agent. The external package itself is not redistributed by
Hobot Code and remains subject to its own license and delivery terms.
