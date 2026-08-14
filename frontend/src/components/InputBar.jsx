import React, { useState, useRef } from 'react';

export default function InputBar({ onSend, loading, onAbort }) {
  const [text, setText] = useState('');
  const taRef = useRef(null);

  const autoResize = () => {
    const ta = taRef.current;
    if (!ta) return;
    ta.style.height = 'auto';
    ta.style.height = Math.min(ta.scrollHeight, 200) + 'px';
  };

  const handleSend = () => {
    const msg = text.trim();
    if (!msg || loading) return;
    setText('');
    if (taRef.current) taRef.current.style.height = 'auto';
    onSend(msg);
  };
  const handleKeyDown = e => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };
  return (
    <div className="input-row">
      <div className="chat-input-box">
        <textarea
          id="input"
          ref={taRef}
          value={text}
          onChange={e => { setText(e.target.value); autoResize(); }}
          onKeyDown={handleKeyDown}
          placeholder="输入消息..."
          disabled={loading}
          rows={1}
        />
        <div className="chat-input-footer">
          <span className="chat-input-hint">{loading ? '正在生成…' : 'Enter 发送，Shift + Enter 换行'}</span>
          <div className="chat-input-actions">
            {loading ? (
              <button className="send-btn stop" onClick={onAbort} title="停止生成" aria-label="停止">
                <svg viewBox="0 0 24 24" width="16" height="16"><rect x="6" y="6" width="12" height="12" rx="2" fill="currentColor"/></svg>
              </button>
            ) : (
              <button className="send-btn" onClick={handleSend} disabled={!text.trim()} title="发送" aria-label="发送">
                <svg viewBox="0 0 24 24" width="18" height="18"><path d="M12 4l-8 8h5v8h6v-8h5z" fill="currentColor"/></svg>
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
