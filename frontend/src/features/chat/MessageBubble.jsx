import React, { useState } from 'react';
import { extractAssistantContent, hasProcessTag, hasBehaviorTag, hasReportTag, hasSummaryTag, extractSummaryContent, getBehaviorDes, getProcessMessage } from '../../api/chat';
import ReportCard from '../../components/ReportCard';
import ToolBlock from '../../components/ToolBlock';

export function filterForResultsOnly(msg) {
  if (msg.role === 'user') return msg;
  if (msg.role === 'tool') return null;
  if (msg.role === 'assistant') {
    if (hasSummaryTag(msg.content || '')) return null;
    if (hasBehaviorTag(msg.content || '')) return msg;
    if (msg.tool_calls && msg.tool_calls.length > 0) return null;
    return msg;
  }
  return msg;
}

function ToolCallView({ tc, result }) {
  return <ToolBlock tc={tc} result={result} />;
}

// Render a standalone tool result message (role === 'tool') as a collapsible block.
function ToolResultMessage({ msg }) {
  const fakeTc = { function: { name: msg.name || 'tool' } };
  return <ToolBlock tc={fakeTc} result={{ content: msg.content || '' }} />;
}

// Collapsible card for a <summary> (compressed history) message.
function SummaryCard({ content }) {
  const [open, setOpen] = useState(false);
  const body = extractSummaryContent(content);
  const preview = body.slice(0, 120);
  return (
    <div className="summary-card">
      <div className="summary-card-header" onClick={() => setOpen(!open)}>
        <span className="summary-card-toggle">{open ? '▾' : '▸'}</span>
        <span className="summary-card-icon"><svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><line x1="8" y1="13" x2="16" y2="13"/><line x1="8" y1="17" x2="16" y2="17"/></svg></span>
        <span className="summary-card-title">历史已压缩</span>
        <span className="summary-card-btn">{open ? '收起' : '展开查看'}</span>
      </div>
      {!open ? (
        <div className="summary-card-preview">{preview}{body.length > 120 ? '...' : ''}</div>
      ) : (
        <pre className="summary-card-body">{body}</pre>
      )}
    </div>
  );
}

function AssistantContent({ content, toolCalls, toolResults = [], suppressReport = false, draft = false, replacedDraft = false }) {
  // <summary> is an internal AI context marker (compressed history); the full
  // conversation is persisted separately and rendered normally, so hide it.
  if (hasSummaryTag(content)) {
    return null;
  }
  if (suppressReport && hasReportTag(content)) {
    return null;
  }
  if (hasReportTag(content)) {
    const body = extractAssistantContent(content);
    const card = body.length > 200 || /!\[|<cite/i.test(body)
      ? <ReportCard bodyText={body} />
      : <pre>{body}</pre>;
    return (
      <div>
        {draft && <div className="report-draft-line">✍️ 草稿中…（反思后将更新为最终版）</div>}
        {card}
        {replacedDraft && <div className="report-updated-line">✅ 反思完成，已更新为最终报告</div>}
      </div>
    );
  }

  // Extract plain text after behavior/process tags
  const stripTags = (text) => {
    return text.replace(/<process[\s\S]*?<\/process>\s*/gi, '')
               .replace(/<behavior[\s\S]*?<\/behavior>\s*/gi, '')
               .replace(/<response[\s\S]*?<\/response>\s*/gi, '')
               .trim();
  };

  return (
    <div>
      {hasProcessTag(content) ? (
        <div>
          <pre className="behavior-text">{getProcessMessage(content) || extractAssistantContent(content)}</pre>
          {stripTags(content) && <pre>{stripTags(content)}</pre>}
        </div>
      ) : hasBehaviorTag(content) ? (
        <div>
          <pre className="behavior-text">{getBehaviorDes(content) || extractAssistantContent(content)}</pre>
          {stripTags(content) && <pre>{stripTags(content)}</pre>}
        </div>
      ) : content && <pre>{content}</pre>}
      {toolCalls && toolCalls.length > 0 && (
        <div className="tool-calls-group">
          {toolCalls.map((tc, i) => <ToolCallView key={i} tc={tc} result={toolResults[i]} />)}
        </div>
      )}
    </div>
  );
}

export default function MessageBubble({ msg, mode }) {
  if (mode === 'results-only') {
    const filtered = filterForResultsOnly(msg);
    if (!filtered) return null;
  }
  const role = msg.role;
  return (
    <div className={'msg msg-' + role}>
      {role === 'user' && <div className="role-label">{role}</div>}
      <div className="bubble">
        {role === 'user' && <pre>{msg.content}</pre>}
        {role === 'user' && msg.real_ms ? <div className='stats-per-message'>⏱ real {(msg.real_ms/1000).toFixed(1) + 's'}</div> : null}
        {role === 'assistant' && <>
        <AssistantContent content={msg.content || ''} toolCalls={msg.tool_calls} toolResults={msg._pairedResults || msg._streamResults} suppressReport={msg._suppressReport} draft={msg.draft} replacedDraft={msg.replacedDraft} />
        {msg.usage && (
          <div className='stats-per-message'>
            ↑ {msg.usage.prompt_tokens ?? '?'} in &nbsp;↓ {msg.usage.completion_tokens ?? '?'} out &nbsp;
            ⏱ agent {msg.duration_ms ? (msg.duration_ms/1000).toFixed(1) + 's' : '?'}
          </div>
        )}
      </>}
        {role === 'tool' && !msg._suppress && <ToolResultMessage msg={msg} />}
        {role === 'system' && <pre className="system-text">{msg.content}</pre>}
      </div>
    </div>
  );
}

