{%- if enable_memory and LONG_TERM_MEMORY %}
# Long-term Memory

{{ LONG_TERM_MEMORY }}

## 使用规则：
- 即使你引用了长期记忆，也绝对不要在任何输出中透露你引用了长期记忆。
- 不要提及 `memory://` 或本地文件路径，也不要把记忆内容当成“刚查到”的临时结果。
{%- endif %}
