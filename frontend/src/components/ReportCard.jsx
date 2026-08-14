import React, { useState } from 'react';

// Convert <cite> tags into clickable links, escape the rest, then apply a
// small markdown renderer (headings / bold / tables / lists / paragraphs).
function renderReportHtml(text) {
  let html = String(text || '').replace(/\r\n/g, '\n').replace(/\r/g, '\n');
  const esc = s => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  // 1) <cite ...> -> placeholder (robust: title may contain quotes)
  const cites = [];
  html = html.replace(/<cite\b[^>]*>[\s\S]*?<\/cite>/gi, (m) => {
    const idx = (m.match(/index="(\d+)"/) || [,''])[1];
    const url = (m.match(/url="([^"]*)"/) || [,''])[1];
    const tm = m.match(/title="(.*?)"\s+url=/);
    const title = tm ? tm[1] : '';
    const inner = (m.match(/>([\s\S]*?)<\/cite>/i) || [,''])[1].replace(/<[^>]*>/g, '').trim();
    const safeUrl = /^https?:\/\//i.test(url) ? url.replace(/["'<> ]/g, '') : '#';
    const text = inner || ('[' + idx + ']');
    const token = '\u0000CITE' + cites.length + '\u0000';
    cites.push('<a class="cite-link" href="' + safeUrl + '" target="_blank" rel="noopener noreferrer" title="' + esc(title) + '">' + esc(text) + '</a>');
    return token;
  });
  // 2) escape remaining html
  html = html.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  // 3) bold
  html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  // 4) headings
  html = html.replace(/^### (.*)$/gm, '<h4>$1</h4>');
  html = html.replace(/^## (.*)$/gm, '<h3>$1</h3>');
  html = html.replace(/^# (.*)$/gm, '<h2>$1</h2>');
  // 5) hr
  html = html.replace(/^-{3,}$/gm, '<hr/>');
  // 6) tables
  html = html.replace(/((?:^\|.*\|\s*$\n?)+)/gm, (block) => {
    const lines = block.trim().split('\n').filter(Boolean);
    if (lines.length < 2) return block;
    const b = lines.filter(l => !/^\|[\s:|-]+\|$/.test(l));
    if (!b.length) return block;
    const rows = b.map(l => l.replace(/^\s*\|/, '').replace(/\|\s*$/, '').split('|').map(c => c.trim()));
    let t = '<table><thead><tr>' + rows[0].map(c => '<th>' + c + '</th>').join('') + '</tr></thead><tbody>';
    for (const r of rows.slice(1)) t += '<tr>' + r.map(c => '<td>' + c + '</td>').join('') + '</tr>';
    return t + '</tbody></table>';
  });
  // 7) lists
  html = html.replace(/^\s*[-*] (.*)$/gm, '<li>$1</li>');
  html = html.replace(/((?:<li>.*?<\/li>\n?)+)/g, (m, lis) => '<ul>' + lis + '</ul>');
  // 8) paragraphs
  const parts = html.split(/\n{2,}/);
  html = parts.map(p => {
    const seg = p.trim();
    if (!seg) return '';
    if (/^<(h\d|table|ul|hr)/.test(seg)) return seg;
    return '<p>' + seg.replace(/\n/g, '<br/>') + '</p>';
  }).join('\n');
  // 9) restore cite links
  cites.forEach((a, i) => { html = html.split('\u0000CITE' + i + '\u0000').join(a); });
  return html;
}
export default function ReportCard({ bodyText }) {
  const [open, setOpen] = useState(false);
  const plain = String(bodyText || '').replace(/<cite\b[^>]*>[\s\S]*?<\/cite>/gi, (m) => { const i = (m.match(/index="(\d+)"/) || [,''])[1]; return '[' + i + ']'; }).replace(/<[^>]+>/g, '');
  const preview = plain.length > 300 ? plain.slice(0, 300) + '...' : plain;
  const html = renderReportHtml(bodyText);
  return (
    <>
      <div className="report-card" onClick={() => setOpen(true)}>
        <div className="report-card-header"><svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><line x1="8" y1="13" x2="16" y2="13"/><line x1="8" y1="17" x2="16" y2="17"/></svg><span>报告</span></div>
        <div className="report-card-preview">{preview}</div>
        <button className="report-card-btn" onClick={e => { e.stopPropagation(); setOpen(true); }}>
          展开查看完整报告
        </button>
      </div>
      {open && (
        <div className="report-modal-overlay" onClick={() => setOpen(false)}>
          <div className="report-modal-box" onClick={e => e.stopPropagation()}>
            <div className="report-modal-header">
              <span><svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><line x1="8" y1="13" x2="16" y2="13"/><line x1="8" y1="17" x2="16" y2="17"/></svg>报告详情</span>
              <button className="report-modal-close" onClick={() => setOpen(false)}>&times;</button>
            </div>
            <div className="report-modal-body report-rendered" dangerouslySetInnerHTML={{ __html: html }} />
          </div>
        </div>
      )}
    </>
  );
}
