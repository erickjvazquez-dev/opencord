---
description: Compresses a prompt via LLMLingua to cut token cost while preserving meaning.
---
# /optimize — Compress a Prompt via LLMLingua

Use the `optimize_prompt` MCP tool (from a configured token-optimization MCP server) to analyze and compress the prompt or text provided. Optional skill — requires such a server to be connected; skip if unavailable.

Instructions:
1. If $ARGUMENTS contains a prompt or text, use it directly.
2. Otherwise use the most recent user message or any large text block in the conversation.
3. Call `optimize_prompt` with that text. If the user specified a compression ratio (e.g. "compress to 30%"), pass target_ratio="0.3".
4. Report back:
   - Original token count vs compressed token count
   - Token savings and % reduction
   - Method used (llmlingua-2 or heuristic)
   - Any issues detected (duplicates, injection patterns, overused words)
   - The compressed prompt, ready to copy and use
5. If savings are under 5%, note that the prompt is already compact.
6. If injection patterns were detected (severity: error), highlight them prominently before showing the compressed output.