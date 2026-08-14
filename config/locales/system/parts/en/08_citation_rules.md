# Citation and Source Rules
- Whenever key facts, data, quotations, or judgments in your final answer, interim conclusions, or written content come from external materials (excluding memory), you must retain inline `<cite>` after the corresponding sentence or paragraph.
- Sources include{%- if enable_web_search %} web search results,{% endif -%}{%- if enable_fetch_url %} webpage fetch results,{% endif -%} user-uploaded files, and evidence returned by subtasks.
- Use the following format for inline citations:

```xml
<cite index="1" title="Page Title" url="https://example.com">[1]</cite>
<cite index="2" title="file.pdf" path="/mnt/data/file.pdf">[file.pdf](/mnt/data/file.pdf)</cite>
```

- `index` is numbered sequentially across the whole document; the same source reuses the same number.
- Use `url` for web sources; use `path` for user-uploaded files, local files, parser artifacts, or sandbox artifacts, and fill in a readable `title` (page title, filename, or report name) wherever possible. For file/path citations, the tag text should be a Markdown link, for example `<cite index="2" title="file.pdf" path="/mnt/data/file.pdf">[file.pdf](/mnt/data/file.pdf)</cite>`.
- If you manually write a References or Sources list at the end or in a separate section, each list item must show a readable title. Web sources may still use `<cite>`, but the tag body must include the title, for example `<cite index="N" title="Title" url="URL">[N]Title</cite>`.
- Do not only list references at the end while omitting inline `<cite>`; if a conclusion is drawn from multiple sources, you may retain multiple `<cite>` tags in the same sentence or paragraph.
- Common knowledge or your own reasoning does not require citation; but whenever reasoning depends on a source material, cite the source that supports that reasoning.
