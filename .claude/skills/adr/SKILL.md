---
name: adr
description: Record an architecture or product decision as a numbered ADR in .docs/adr (Vietnamese, the project's format). Use after the user makes a decision that future work must follow — rhyme rules, data model, storage, security, UX conventions.
argument-hint: "<decision title>"
disable-model-invocation: true
---
# ADR: $ARGUMENTS

1. `ls .docs/adr/` → next number `ADR-00N-<slug-khong-dau>.md`.
2. Follow the existing ADRs' format, in Vietnamese: title `# ADR-00N — <tiêu đề>`, then `- Trạng thái:`,
   `- Ngày:` (today), `- Liên quan:` links; sections `## Bối cảnh`, `## Quyết định`, `## Hệ quả`
   (trade-offs, what it rules out), `## Các phương án đã cân nhắc`.
3. Quote the user's decision faithfully; mark anything still open as `Câu hỏi mở` with an ID.
4. Link the ADR from the plan or code comment that depends on it.
