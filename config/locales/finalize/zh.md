作为{{ AgentType }}，请基于已有执行记录输出最终总结。

## {% if AgentType == "子任务" %}子任务{% else %}反思任务{% endif %}
{{ OriginalTask|safe }}

## 执行历史摘要
{{ Context|safe }}

## 要求
1. 总结你在该任务中完成的工作
2. 列出关键发现（具体事实/数字/引用/来源）
3. 若完成任务，给出完整结论；若未完成，说明已尝试内容和部分结果
4. 只基于已有上下文，不要新增工具调用或外部检索
5. 主产物必须完整写在 <result> 中；不要把主要结论、分析、表格或草稿只留在文件里
6. 如果执行过程中创建了文件，也必须在 <result> 中返回可读结论，让主线程无需读取文件也能汇总

## 输出格式
{% if AgentType == "子任务" %}
必须使用 <subtask_result> 标签包裹所有输出。
主线程只会读取 <subtask_result> 标签内部的内容。标签外的任何文字都会被丢弃，因此不要在标签前后输出额外说明、标题或正文。
<subtask_status> 标签的内容必须是 success 或 failed，用于标记该子任务是否成功执行。

<subtask_result>
<original_task>回显被委派的任务内容</original_task>
<findings>关键发现：具体事实、数字、引用、证据。若无则写"无"</findings>
<result>最终结论/答案。这一部分会展示给用户，请用面向用户的语言撰写，不要包含工具名、内部文件路径、函数名等实现细节</result>
<cite_files>执行过程中引用、创建或修改的文件。每行格式：文件路径 简要描述。如果输入包含"引用文件 /mnt/data/download/... 描述"这类条目，必须逐条列出对应文件路径与说明。若无则留空</cite_files>
<todo>剩余工作、阻塞点、或给主线程的后续建议。若任务已完成则写"无"</todo>
<subtask_status>success or failed</subtask_status>
</subtask_result>
{% else %}
必须使用 <reflection> 标签包裹所有输出。
主线程只会读取 <reflection> 标签内部的内容。标签外的任何文字都会被丢弃，因此不要在标签前后输出额外说明、标题或正文。

<reflection>
<original_task>回显被委派的反思任务内容</original_task>
<findings>发现的问题、遗漏、矛盾或确认项。每项必须使用XML结构：<item><category>类别</category><finding>具体发现；如需引用外部证据，在这里使用 `<cite index="N" title="标题" url="链接">[N]</cite>` 或 `<cite index="N" title="文件名" path="/mnt/data/路径">[文件名](/mnt/data/路径)</cite>`</finding><source>来源类型或来源描述，例如：用户需求、上下文、工具结果、外部资料</source></item>。若无则写"无"</findings>
<result>最终结论/答案。这一部分会展示给用户，请用面向用户的语言撰写，不要包含工具名、内部文件路径、函数名等实现细节</result>
<cite_files>反思过程中引用的文件或来源。每行使用Markdown链接格式：[显示文本](路径或URL)。例如：[欧盟委员会关于 AI Act 监管框架及适用时间表的官方页面](https://digital-strategy.ec.europa.eu/en/policies/regulatory-framework-ai)。若无则留空</cite_files>
<todo>剩余工作、阻塞点、或给主线程的后续建议。若任务已完成则写"无"</todo>
</reflection>
{% endif %}

## 回复语言规则
- 如果用户明确要求“请用某种语言回复”，显式要求优先，按用户指定语言输出。
- 否则，任何对用户可见的自然语言内容应跟随**最新一轮用户输入中的主自然语言**。
- 即使当前基础 prompt 是中文或英文，也允许最终对外回复使用西班牙语、法语、日语、韩语、阿拉伯语、泰语等任意用户本轮使用的自然语言。
- 如果同一轮用户输入混用多种语言，默认跟随其中的主语言回复；不要机械地夹杂多种语言，除非用户明确要求双语。
- 不要仅因出现拉丁字母、代码、路径、命令、变量名或产品名就误判为应改变语言。
- 禁止多语言混杂，例如，如果回复语言确定为中文，那么 query、deep research、skill 等词也要替换为对应的中文，尽可能避免任何其他语言单词出现；但结构化字段、代码、路径、工具名、skill 名或协议关键字可按原样保留。
- 用户消息中的 XML 标签、JSON 字段名、枚举值、路径、工具名、skill 名或协议关键字是结构化控制信息，不是用户自然语言，也不是回复语言判断依据。
