import React, { useState, useCallback, useEffect } from 'react';

const optionColors = ['#E8F4FD', '#FFF3E0', '#E8F5E9', '#FCE4EC', '#F3E5F5'];

export default function AskModal({ questions, askType, onComplete, onSkipAll }) {
  const [currentIdx, setCurrentIdx] = useState(0);
  const [answers, setAnswers] = useState({});
  const [freeText, setFreeText] = useState('');
  const [secondsLeft, setSecondsLeft] = useState(30); // 30s 无应答自动选择倒计时

  const q = questions[currentIdx];
    if (!q) return (
    <div className="ask-modal-overlay">
      <div className="ask-modal-card confirm-card">
        <div className="ask-modal-header">
          <div className="ask-modal-title">确认操作</div>
          <span className="ask-modal-progress">确认</span>
        </div>
        <div className="ask-modal-confirm-body">
          <p>是否继续执行？</p>
        </div>
        <div className="ask-modal-footer">
          <span className="ask-modal-skip" onClick={onSkipAll}>跳过</span>
          <button className="ask-modal-next" onClick={() => onComplete({confirmed:true,askType:askType})}>
            确认
          </button>
        </div>
      </div>
    </div>
  );

  const isLast = currentIdx === questions.length - 1;
  const selected = answers[q.id] || [];
  const isMulti = q.multi_select === true;

  const toggleOption = (opt) => {
    setSecondsLeft(30); // 用户操作 -> 重置倒计时
    const label = opt.label || opt;
    setAnswers(prev => {
      const prevSel = prev[q.id] || [];
      if (isMulti) {
        const next = prevSel.includes(label)
          ? prevSel.filter(x => x !== label)
          : [...prevSel, label];
        return { ...prev, [q.id]: next };
      } else {
        return { ...prev, [q.id]: [label], [`${q.id}_desc`]: opt.description || '' };
      }
    });
  };

  const handleNext = useCallback(() => {
    if (freeText.trim()) {
      setAnswers(prev => ({ ...prev, [`${q.id}_free_text`]: freeText.trim() }));
    }
    if (isLast || currentIdx >= questions.length - 1) {
      const finalAnswers = { ...answers };
      if (freeText.trim()) finalAnswers[`${q.id}_free_text`] = freeText.trim();
      setFreeText('');
      onComplete(finalAnswers);
    } else {
      setFreeText('');
      setCurrentIdx(prev => prev + 1);
    }
  }, [currentIdx, freeText, answers, q, isLast, questions, onComplete]);

  // 30s 无应答：自动勾选当前问题第一项并推进（最后一题直接完成）。
  const autoProceed = useCallback(() => {
    const first = q && q.options && q.options[0];
    const label = first ? (first.label || first) : null;
    const autoAnswers = { ...answers };
    if (label) {
      if (isMulti) {
        autoAnswers[q.id] = (autoAnswers[q.id] || []).includes(label) ? autoAnswers[q.id] : [label];
      } else {
        autoAnswers[q.id] = [label];
        if (first.description) autoAnswers[q.id + '_desc'] = first.description;
      }
    }
    if (freeText.trim()) autoAnswers[q.id + '_free_text'] = freeText.trim();
    if (isLast || currentIdx >= questions.length - 1) {
      setFreeText('');
      onComplete(autoAnswers);
    } else {
      setFreeText('');
      setAnswers(autoAnswers);
      setCurrentIdx(prev => prev + 1);
      setSecondsLeft(30);
    }
  }, [q, isMulti, answers, freeText, isLast, currentIdx, questions, onComplete]);

  const resetTimer = useCallback(() => setSecondsLeft(30), []);

  useEffect(() => {
    if (secondsLeft <= 0) {
      autoProceed();
      return;
    }
    const timer = setTimeout(() => setSecondsLeft(s => s - 1), 1000);
    return () => clearTimeout(timer);
  }, [secondsLeft, autoProceed]);

  return (
    <div className="ask-modal-overlay">
      <div className="ask-modal-card">
        {/* Header */}
        <div className="ask-modal-header">
          <div className="ask-modal-title">{q.question || q.title || ''}</div>
          {isMulti && <span className="ask-modal-tag">多选</span>}
          <span className="ask-modal-progress">{currentIdx + 1} / {questions.length}</span>
        </div>

        {/* Options */}
        <div className="ask-modal-options">
          {q.options && q.options.map((opt, oi) => {
            const label = opt.label || opt;
            const desc = opt.description || '';
            const isSel = selected.includes(label);
            return (
              <div
                key={oi}
                className={`ask-option-card ${isSel ? 'selected' : ''}`}
                style={{ '--card-color': optionColors[oi % optionColors.length] }}
                onClick={() => toggleOption(opt)}
              >
                <div className="ask-option-num">{oi + 1}</div>
                <div className="ask-option-body">
                  <div className="ask-option-label">{label}</div>
                  {desc && <div className="ask-option-desc">{desc}</div>}
                </div>
                <div className="ask-option-check">{isSel ? '✓' : ''}</div>
              </div>
            );
          })}
        </div>

        {/* Free text input */}
        <div className="ask-modal-free-text">
          <input
            type="text"
            placeholder={q.allow_free_text !== false ? '告诉小浣熊你的想法' : ''}
            value={freeText}
            onChange={e => { setFreeText(e.target.value); resetTimer(); }}
            onKeyDown={e => { if (e.key === 'Enter') handleNext(); }}
          />
        </div>

        {/* Footer */}
        <div className="ask-modal-footer">
          <span className="ask-modal-skip" onClick={onSkipAll}>跳过全部</span>
          <span className="ask-modal-timer">{secondsLeft > 0 ? secondsLeft + 's ' : ''}无操作自动选择</span>
          <button className="ask-modal-next" onClick={handleNext}>
            {isLast ? '完成' : '下一步'}
          </button>
        </div>
      </div>
    </div>
  );
}
