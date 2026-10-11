---
description: Token-compresses files through an MCP preprocessing server before they are sent to a model.
---
# /preprocess — Token-Compress Files via an MCP Server

Use the `preprocess_files` MCP tool (from a configured file-preprocessing MCP server) to preprocess any files mentioned in this conversation or provided as arguments. Optional skill — requires such a server to be connected; skip if unavailable.

Instructions:
1. Identify all file paths referenced — from the user's message, $ARGUMENTS, or recent conversation context.
2. If $ARGUMENTS contains file paths or glob patterns, use those directly.
3. Call `preprocess_files` with those paths, output_format="markdown", and use_cache=true.
4. Present the preprocessed content clearly, noting:
   - Which processor was used for each file (e.g. pdfplumber, easyocr, csv)
   - Token estimate for each file
   - Whether the result came from cache
   - Total token estimate across all files
5. If no files are identified, ask the user which files to preprocess.
6. If a processor is unavailable, explain what needs to be installed and offer the cloud fallback option if applicable.

The goal is to convert raw files into token-efficient structured text before answering questions about them.