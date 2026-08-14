import React from 'react';

export default function Sidebar({ sessions, sessionName, onSelect, onNew, onDelete }) {
  return (
    <aside className="sidebar">
      <div className="sidebar-brand">
        <img className="sidebar-brand-logo" src="/fairy.png" alt="Fairy" draggable={false} />
        <div className="sidebar-brand-text">
          <strong>FAIRY</strong>
          <span>Agent Workbench</span>
        </div>
      </div>
      <h2>历史对话</h2>
      <button className="new-btn" onClick={onNew}>+ 新对话</button>
      <div className="session-list">
        {sessions.length === 0 && <div className="empty-list">暂无历史对话</div>}
        {sessions.map(s => (
          <div
            key={s.name}
            className={'session' + (s.name === sessionName ? ' active' : '')}
            onClick={() => onSelect(s.name)}
          >
            <div className={'preview' + (s.preview ? '' : ' empty-preview')}>
              {s.preview || '(新对话，空的)'}
            </div>
            <div className="meta">
              {s.model && <span className="model-badge">{s.model}</span>}
              <span>{s.message_count}条 · {s.modified ? s.modified.slice(11, 19) : ''}</span>
            </div>
            <button className="delete" onClick={e => { e.stopPropagation(); onDelete(s.name); }} title="删除会话">×</button>
          </div>
        ))}
      </div>
    </aside>
  );
}
