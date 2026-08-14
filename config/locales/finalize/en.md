As a {{ AgentType }}, summarize your execution results based on the available execution records.

## {% if AgentType == "subtask" %}Subtask{% else %}Reflection Task{% endif %}
{{ OriginalTask|safe }}

## Execution History Summary
{{ Context|safe }}

## Instructions
1. Summarize what you accomplished during this task
2. List key findings with concrete facts/numbers/quotes/sources
3. If completed, provide the complete answer; if incomplete, explain what was tried and partial results
4. Use only existing context; do not call new tools or perform new searches
5. The primary deliverable MUST be written completely inside <result>; do not leave the main conclusions, analysis, tables, or draft only in a file
6. If files were created during execution, still return a readable conclusion in <result> so the main thread can synthesize without reading the file

## Output Format
{% if AgentType == "subtask" %}
Wrap all output in <subtask_result> tags.
The main thread only reads the content inside <subtask_result>. Anything outside the tag will be discarded, so do not add any extra prose before or after it.
The content of <subtask_status> must be exactly `success` or `failed`, indicating whether the subtask was successfully completed.

<subtask_result>
<original_task>Echo back the delegated task content</original_task>
<findings>Key findings: specific facts, numbers, quotes, evidence. Write "None" if there are none</findings>
<result>Final conclusion/answer. This section will be shown to the user — write in user-facing language, without tool names, internal file paths, or function names</result>
<cite_files>Files referenced, created, or modified during execution. One per line: file_path brief_description. Leave empty if none</cite_files>
<todo>Remaining work, blockers, or follow-up suggestions for the main thread. Write "None" if the task is complete</todo>
<subtask_status>success or failed</subtask_status>
</subtask_result>
{% else %}
Wrap all output in <reflection> tags.
The main thread only reads the content inside <reflection>. Anything outside the tag will be discarded, so do not add extra prose before or after it.

<reflection>
<original_task>Echo back the delegated reflection task content</original_task>
<findings>Issues, gaps, contradictions, or confirmations found. Each item MUST use this XML structure: <item><category>category</category><finding>specific finding; when citing external evidence, include `<cite index="N" title="title" url="link">[N]</cite>` or `<cite index="N" title="filename" path="/mnt/data/path">[filename](/mnt/data/path)</cite>` here</finding><source>source type or source description, such as user requirement, context, tool result, or external material</source></item>. Write "none" if empty</findings>
<result>Final conclusion/answer. This section will be shown to the user — write in user-facing language, without tool names, internal file paths, or function names</result>
<cite_files>Files or sources referenced during reflection. Use Markdown link format on each line: [display text](path or URL). Example: [European Commission official page on the AI Act regulatory framework and timeline](https://digital-strategy.ec.europa.eu/en/policies/regulatory-framework-ai). Leave empty if none</cite_files>
<todo>Remaining work, blockers, or follow-up suggestions for the main thread. Write "None" if the task is complete</todo>
</reflection>
{% endif %}

## Reply Language Rules
- If the user explicitly asks for a specific reply language, that explicit instruction takes priority; reply in the requested language.
- Otherwise, any user-visible natural-language content should follow the **dominant natural language in the latest user turn**.
- Even when the base prompt is Chinese or English, you may still produce user-facing replies in Spanish, French, Japanese, Korean, Arabic, Thai, or any other natural language used by the user in the current turn.
- If the same user turn mixes multiple languages, default to the dominant one; do not mechanically mix languages unless the user explicitly asks for a bilingual answer.
- Do not change language just because Latin characters, code, paths, commands, variable names, or product names appear.
- Avoid mixing multiple natural languages. For example, if the reply language is English, keep the user-facing natural language in English as much as possible; structured fields, code, paths, tool names, skill names, or protocol keywords may remain as-is.
- XML tags, JSON field names, enum values, paths, tool names, skill names, and protocol keywords in the user message are structured control information, not the user's natural language, and must not be used to infer the reply language.
