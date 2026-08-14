import React, { useState } from 'react';

const modes = [
  { value: 'no-template', label: '无模板模式', description: '由 AI 自由设计简洁的商务或学术版式' },
  { value: 'template', label: '模板模式', description: '使用预设模板生成演示文稿' },
  { value: 'creative', label: '创意模式', description: '使用更具视觉设计感的创意排版' },
];

const templates = [
  { value: 'white', label: '简洁白色', description: '适合通用商务与汇报' },
  { value: 'tech-blue', label: '科技蓝色', description: '适合技术与产品主题' },
];

const initialConfig = {
  role: '演讲者',
  scene: '通用演示',
  audience: '普通受众',
  page_count_desc: '10 页左右',
  ppt_mode: 'no-template',
  template_name: 'white',
};

// The production UI returns a structured PPT parameter payload for this ask
// type. Keep the local harness equally explicit instead of turning the reply
// into an ordinary natural-language answer that the PPT Skill cannot consume.
export default function PPTConfigModal({ onComplete, onSkipAll }) {
  const [config, setConfig] = useState(initialConfig);
  const update = (key, value) => setConfig(prev => ({ ...prev, [key]: value }));

  return (
    <div className="ask-modal-overlay">
      <div className="ask-modal-card ppt-config-modal">
        <div className="ask-modal-header">
          <div className="ask-modal-title">确认 PPT 制作参数</div>
          <div className="ppt-config-hint">这些参数只用于当前这份演示文稿。</div>
        </div>

        <div className="ppt-mode-options">
          {modes.map(mode => (
            <label className={`ppt-mode-option ${config.ppt_mode === mode.value ? 'selected' : ''}`} key={mode.value}>
              <input
                type="radio"
                name="ppt-mode"
                value={mode.value}
                checked={config.ppt_mode === mode.value}
                onChange={event => update('ppt_mode', event.target.value)}
              />
              <span><strong>{mode.label}</strong><small>{mode.description}</small></span>
            </label>
          ))}
        </div>

        <div className="ppt-config-fields">
          <label>演讲者身份<input value={config.role} onChange={event => update('role', event.target.value)} /></label>
          <label>使用场景<input value={config.scene} onChange={event => update('scene', event.target.value)} /></label>
          <label>目标受众<input value={config.audience} onChange={event => update('audience', event.target.value)} /></label>
          <label>页数<input value={config.page_count_desc} onChange={event => update('page_count_desc', event.target.value)} /></label>
        </div>

        {config.ppt_mode === 'template' && (
          <div className="ppt-template-options">
            <div className="ppt-template-title">选择本地模板</div>
            {templates.map(template => (
              <label className={`ppt-template-option ${config.template_name === template.value ? 'selected' : ''}`} key={template.value}>
                <input
                  type="radio"
                  name="ppt-template"
                  value={template.value}
                  checked={config.template_name === template.value}
                  onChange={event => update('template_name', event.target.value)}
                />
                <span><strong>{template.label}</strong><small>{template.description}</small></span>
              </label>
            ))}
          </div>
        )}

        <div className="ask-modal-footer">
          <span className="ask-modal-skip" onClick={onSkipAll}>取消</span>
          <button className="ask-modal-next" onClick={() => onComplete({ pptConfig: config })}>确认并开始</button>
        </div>
      </div>
    </div>
  );
}
