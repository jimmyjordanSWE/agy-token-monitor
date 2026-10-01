---
description: Critical automatic context handoff directive
---

# ⚠️ CRITICAL CONTEXT LIMIT DIRECTIVE
Notice: The current conversation has reached its context limit or the user has requested a handoff.

## INSTRUCTIONS FOR ASSISTANT:
1. Complete your immediate sub-task cleanly. Do NOT initiate new large refactors or long investigative tangents.
2. Ensure working state is verified (git status, clean files).
3. Conclude your turn with a complete, self-contained **HANDOFF SUMMARY** enclosed entirely inside a single copyable Markdown code block:
   ```markdown
   # Hand-off Summary
   ... complete status, modified files, git state, next actions ...
   ```
   Do not leave sections outside the code block; the entire summary must be easily copyable with one click.
