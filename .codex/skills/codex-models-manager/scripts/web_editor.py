import argparse
import http.server
import json
import os
import socketserver
import sys
import threading
import urllib.parse
import webbrowser
from typing import Any, Dict

import model_catalog_core as core

HTML_TEMPLATE = r'''<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Codex 模型配置管理器 (Visual Catalog Manager)</title>
<style>
:root {
  --bg: #0d1117;
  --surface: #161b22;
  --surface-hover: #1c2128;
  --border: #30363d;
  --border-active: #58a6ff;
  --text: #c9d1d9;
  --text-muted: #8b949e;
  --text-heading: #f0f6fc;
  --primary: #238636;
  --primary-hover: #2ea043;
  --blue: #1f6feb;
  --blue-hover: #388bfd;
  --danger: #da3633;
  --danger-hover: #f85149;
  --warning: #d29922;
  --badge-bg: #21262d;
  --radius: 6px;
  --font: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
}
* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  background: var(--bg);
  color: var(--text);
  font-family: var(--font);
  font-size: 13px;
  line-height: 1.5;
  padding: 0 0 40px 0;
}
header {
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  padding: 12px 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  position: sticky;
  top: 0;
  z-index: 100;
}
.header-title {
  display: flex;
  align-items: center;
  gap: 12px;
}
.header-title h1 {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-heading);
}
.header-badges {
  display: flex;
  gap: 8px;
}
.badge {
  background: var(--badge-bg);
  border: 1px solid var(--border);
  color: var(--text-muted);
  padding: 2px 8px;
  border-radius: 12px;
  font-size: 11px;
}
.badge.success { border-color: rgba(46,160,67,0.4); color: #3fb950; }
.badge.info { border-color: rgba(56,139,253,0.4); color: #58a6ff; }
.header-actions {
  display: flex;
  gap: 8px;
}
.btn {
  background: var(--surface);
  border: 1px solid var(--border);
  color: var(--text);
  padding: 6px 12px;
  border-radius: var(--radius);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  transition: all 0.15s ease;
}
.btn:hover { background: var(--surface-hover); border-color: #8b949e; }
.btn.primary { background: var(--primary); border-color: rgba(240,246,252,0.1); color: #fff; }
.btn.primary:hover { background: var(--primary-hover); }
.btn.blue { background: var(--blue); border-color: rgba(240,246,252,0.1); color: #fff; }
.btn.blue:hover { background: var(--blue-hover); }
.btn.danger { background: transparent; border-color: var(--danger); color: #f85149; }
.btn.danger:hover { background: var(--danger); color: #fff; }
.btn.sm { padding: 3px 8px; font-size: 11px; }

.container {
  max-width: 1280px;
  margin: 16px auto;
  padding: 0 20px;
}
.upstream-bar {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 12px 16px;
  margin-bottom: 16px;
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.form-group-inline {
  display: flex;
  align-items: center;
  gap: 8px;
}
.form-group-inline label {
  color: var(--text-muted);
  font-size: 12px;
  white-space: nowrap;
}
input[type="text"], input[type="number"], select, textarea {
  background: #0d1117;
  border: 1px solid var(--border);
  color: var(--text);
  padding: 5px 10px;
  border-radius: var(--radius);
  font-size: 12px;
  font-family: inherit;
}
input[type="text"]:focus, select:focus, textarea:focus {
  outline: none;
  border-color: var(--border-active);
  box-shadow: 0 0 0 2px rgba(88,166,255,0.2);
}
.nav-tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 16px;
}
.tab-btn {
  background: transparent;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--text-muted);
  padding: 8px 16px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 8px;
}
.tab-btn:hover { color: var(--text); }
.tab-btn.active {
  color: var(--text-heading);
  border-bottom-color: #f78166;
  font-weight: 600;
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
  gap: 12px;
}
.search-input {
  width: 280px;
}

/* Models Table & Cards */
.models-table {
  width: 100%;
  border-collapse: collapse;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  overflow: hidden;
}
.models-table th, .models-table td {
  padding: 10px 14px;
  text-align: left;
  border-bottom: 1px solid var(--border);
}
.models-table th {
  background: #11141a;
  color: var(--text-muted);
  font-weight: 600;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.models-table tbody tr:hover {
  background: var(--surface-hover);
}
.model-slug {
  font-weight: 600;
  color: var(--text-heading);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
}
.model-name {
  color: var(--text);
}
.model-desc {
  color: var(--text-muted);
  font-size: 11px;
  max-width: 300px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tag {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 500;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--text-muted);
}
.tag.reasoning { border-color: rgba(210,153,34,0.4); color: #d29922; }
.tag.shell { border-color: rgba(56,139,253,0.3); color: #58a6ff; }
.tag.green { border-color: rgba(46,160,67,0.4); color: #3fb950; }

/* Modal Dialog */
.modal-backdrop {
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(0,0,0,0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.2s ease;
}
.modal-backdrop.open {
  opacity: 1;
  pointer-events: auto;
}
.modal {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  width: 90%;
  max-width: 720px;
  max-height: 90vh;
  display: flex;
  flex-direction: column;
  box-shadow: 0 16px 32px rgba(0,0,0,0.5);
}
.modal-header {
  padding: 14px 20px;
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.modal-header h2 {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-heading);
}
.modal-close {
  background: transparent;
  border: none;
  color: var(--text-muted);
  font-size: 18px;
  cursor: pointer;
}
.modal-close:hover { color: var(--text); }
.modal-body {
  padding: 16px 20px;
  overflow-y: auto;
  flex: 1;
}
.modal-footer {
  padding: 12px 20px;
  border-top: 1px solid var(--border);
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  background: #12161c;
}

.form-row {
  display: flex;
  gap: 16px;
  margin-bottom: 14px;
}
.form-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.form-col label {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
}
.form-col input, .form-col select, .form-col textarea {
  width: 100%;
}
.form-col small {
  font-size: 11px;
  color: var(--text-muted);
}

/* Toast */
#toast {
  position: fixed;
  bottom: 24px;
  right: 24px;
  background: #1f242c;
  border: 1px solid var(--border);
  color: var(--text);
  padding: 10px 18px;
  border-radius: var(--radius);
  font-size: 12px;
  box-shadow: 0 8px 16px rgba(0,0,0,0.4);
  opacity: 0;
  transform: translateY(10px);
  transition: all 0.25s ease;
  z-index: 2000;
}
#toast.show {
  opacity: 1;
  transform: translateY(0);
}
#toast.success { border-color: rgba(46,160,67,0.6); color: #3fb950; }
#toast.error { border-color: rgba(218,54,51,0.6); color: #f85149; }

.json-editor {
  width: 100%;
  height: 480px;
  background: #090d13;
  color: #79c0ff;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  resize: vertical;
}
</style>
</head>
<body>

<header>
  <div class="header-title">
    <h1>Codex 模型配置管理器</h1>
    <div class="header-badges">
      <span class="badge" id="catalog-path-badge">Catalog: 加载中...</span>
      <span class="badge success" id="configured-count-badge">已启用: 0</span>
      <span class="badge info" id="upstream-count-badge">上游可用: 0</span>
    </div>
  </div>
  <div class="header-actions">
    <button class="btn sm" onclick="loadAllData()">刷新数据</button>
    <button class="btn sm primary" onclick="openAddModelModal()">+ 手动添加模型</button>
  </div>
</header>

<div class="container">
  <!-- 上游源拉取栏 -->
  <div class="upstream-bar">
    <div class="form-group-inline" style="flex: 2; min-width: 280px;">
      <label>模型源 API:</label>
      <input type="text" id="upstream-url" value="http://127.0.0.1:8317/v1/models" style="flex: 1;" placeholder="http://127.0.0.1:8317/v1/models">
    </div>
    <div class="form-group-inline" style="flex: 1.5; min-width: 220px;">
      <label>API Key:</label>
      <input type="text" id="upstream-key" value="sk-U6cnDx0z3xtOGLoZXSGo" style="flex: 1;" placeholder="sk-...">
    </div>
    <button class="btn blue" onclick="fetchUpstream()">拉取可用模型源</button>
  </div>

  <!-- 选项卡 -->
  <div class="nav-tabs">
    <button class="tab-btn active" onclick="switchTab('configured')">
      已启用模型列表 (<span id="tab-configured-num">0</span>)
    </button>
    <button class="tab-btn" onclick="switchTab('upstream')">
      上游模型比对与开启 (<span id="tab-upstream-num">0</span>)
    </button>
    <button class="tab-btn" onclick="switchTab('raw')">
      原始 JSON 编辑
    </button>
    <button class="tab-btn" onclick="switchTab('backups')">
      备份与恢复 (<span id="tab-backups-num">0</span>)
    </button>
  </div>

  <!-- Tab 1: 已配置模型 -->
  <div id="tab-configured" class="tab-content">
    <div class="toolbar">
      <input type="text" class="search-input" id="configured-search" placeholder="搜索模型 slug 或名称..." oninput="renderConfiguredTable()">
      <div style="color: var(--text-muted); font-size: 11px;">
        提示: 支持直接鼠标拖拽表格行排序，或点击 ▲/▼/置顶/置底快捷按钮调整顺序与优先级
      </div>
    </div>
    <table class="models-table">
      <thead>
        <tr>
          <th style="width: 50px; text-align: center;">拖拽</th>
          <th style="width: 70px;">序号 / 优先级</th>
          <th style="width: 220px;">模型标识 (Slug)</th>
          <th style="width: 180px;">显示名称</th>
          <th>思考/推理 (Reasoning)</th>
          <th style="width: 120px;">终端执行器</th>
          <th style="width: 80px;">可见性</th>
          <th style="width: 230px; text-align: right;">排序与操作</th>
        </tr>
      </thead>
      <tbody id="configured-tbody">
        <tr><td colspan="8" style="text-align: center; color: var(--text-muted);">正在加载...</td></tr>
      </tbody>
    </table>
  </div>

  <!-- Tab 2: 上游模型比对 -->
  <div id="tab-upstream" class="tab-content" style="display: none;">
    <div class="toolbar">
      <input type="text" class="search-input" id="upstream-search" placeholder="筛选上游模型..." oninput="renderUpstreamTable()">
      <span style="color: var(--text-muted); font-size: 12px;">直接点击“开启到 Codex”即可快速根据模板加入目录</span>
    </div>
    <table class="models-table">
      <thead>
        <tr>
          <th style="width: 90px;">状态</th>
          <th style="width: 260px;">模型标识 (Slug / ID)</th>
          <th style="width: 140px;">归属 (Owned By)</th>
          <th>Codex 中配置的显示名称</th>
          <th style="width: 140px; text-align: right;">操作</th>
        </tr>
      </thead>
      <tbody id="upstream-tbody">
        <tr><td colspan="5" style="text-align: center; color: var(--text-muted);">请点击上方“拉取可用模型源”按钮</td></tr>
      </tbody>
    </table>
  </div>

  <!-- Tab 3: 原始 JSON 编辑 -->
  <div id="tab-raw" class="tab-content" style="display: none;">
    <div class="toolbar">
      <span style="color: var(--text-muted); font-size: 12px;">直接修改 default.json 完整内容。保存前会自动创建完整安全备份。</span>
      <div>
        <button class="btn sm" onclick="formatRawJson()">格式化 JSON</button>
        <button class="btn sm primary" onclick="saveRawJson()">保存并应用</button>
      </div>
    </div>
    <textarea id="raw-json-text" class="json-editor" spellcheck="false"></textarea>
  </div>

  <!-- Tab 4: 备份管理 -->
  <div id="tab-backups" class="tab-content" style="display: none;">
    <div class="toolbar">
      <span style="color: var(--text-muted); font-size: 12px;">每次添加、更新、删除或保存操作都会自动生成一份时间戳备份。</span>
      <button class="btn sm blue" onclick="createManualBackup()">+ 手动立即备份</button>
    </div>
    <table class="models-table">
      <thead>
        <tr>
          <th style="width: 200px;">备份时间</th>
          <th style="width: 120px;">文件大小</th>
          <th>备份文件名</th>
          <th style="width: 120px; text-align: right;">操作</th>
        </tr>
      </thead>
      <tbody id="backups-tbody">
        <tr><td colspan="4" style="text-align: center; color: var(--text-muted);">正在加载备份列表...</td></tr>
      </tbody>
    </table>
  </div>
</div>

<!-- 编辑模型 Modal -->
<div class="modal-backdrop" id="edit-modal">
  <div class="modal">
    <div class="modal-header">
      <h2 id="modal-title">编辑模型配置</h2>
      <button class="modal-close" onclick="closeEditModal()">&times;</button>
    </div>
    <div class="modal-body">
      <input type="hidden" id="edit-orig-slug">
      <input type="hidden" id="edit-is-add" value="0">

      <div class="form-row" id="template-select-row" style="display: none;">
        <div class="form-col">
          <label>克隆参考模板 (Base Template)</label>
          <select id="edit-template-slug" onchange="onTemplateChange()">
            <option value="">-- 选择模板模型 --</option>
          </select>
          <small>新模型将完整继承该模板的系统指令和架构配置</small>
        </div>
      </div>

      <div class="form-row">
        <div class="form-col">
          <label>模型标识 (Slug / ID) *</label>
          <input type="text" id="edit-slug" placeholder="e.g. deepseek-v4.1-flash">
          <small>必须与 /v1/models 中的模型 ID 完全一致</small>
        </div>
        <div class="form-col">
          <label>显示名称 (Display Name) *</label>
          <input type="text" id="edit-display-name" placeholder="e.g. DeepSeek-V4.1-Flash">
        </div>
      </div>

      <div class="form-row">
        <div class="form-col">
          <label>模型描述 (Description)</label>
          <input type="text" id="edit-description" placeholder="模型能力简述">
        </div>
      </div>

      <div class="form-row">
        <div class="form-col">
          <label>默认思考深度 (Default Reasoning)</label>
          <select id="edit-default-reasoning">
            <option value="none">none</option>
            <option value="low">low</option>
            <option value="medium">medium</option>
            <option value="high">high</option>
            <option value="xhigh">xhigh</option>
            <option value="max">max</option>
          </select>
        </div>
        <div class="form-col">
          <label>终端执行类型 (Shell Type)</label>
          <select id="edit-shell-type">
            <option value="shell_command">shell_command</option>
            <option value="unified_exec">unified_exec</option>
          </select>
        </div>
      </div>

      <div class="form-row">
        <div class="form-col">
          <label>优先级 (Priority 数字越大越靠后)</label>
          <input type="number" id="edit-priority" value="1000">
        </div>
        <div class="form-col">
          <label>下拉可见性 (Visibility)</label>
          <select id="edit-visibility">
            <option value="list">list (在下拉菜单显示)</option>
            <option value="hidden">hidden (隐藏)</option>
          </select>
        </div>
      </div>

      <div class="form-row">
        <div class="form-col">
          <label>上下文上限 (Context Window)</label>
          <input type="number" id="edit-context-window" value="1048576">
        </div>
        <div class="form-col">
          <label>单轮最大输出 (Max Output Tokens)</label>
          <input type="number" id="edit-max-output-tokens" value="131072">
        </div>
        <div class="form-col">
          <label>压缩触发阈值 (Auto Compact Limit)</label>
          <input type="number" id="edit-auto-compact" value="943718">
        </div>
      </div>

      <div class="form-row">
        <div class="form-col">
          <label>支持的思考深度 (Supported Reasoning Levels)</label>
          <div style="display: flex; gap: 12px; margin-top: 6px;" id="reasoning-checkboxes">
            <label><input type="checkbox" value="low"> low</label>
            <label><input type="checkbox" value="medium"> medium</label>
            <label><input type="checkbox" value="high" checked> high</label>
            <label><input type="checkbox" value="xhigh"> xhigh</label>
            <label><input type="checkbox" value="max" checked> max</label>
          </div>
        </div>
      </div>
    </div>
    <div class="modal-footer">
      <button class="btn" onclick="closeEditModal()">取消</button>
      <button class="btn primary" onclick="submitEditForm()">保存配置</button>
    </div>
  </div>
</div>

<div id="toast"></div>

<script>
let state = {
  catalog: { models: [] },
  catalogPath: '',
  upstreamModels: [],
  comparison: null,
  backups: []
};

function showToast(msg, type = 'info') {
  const toast = document.getElementById('toast');
  toast.textContent = msg;
  toast.className = 'show ' + type;
  setTimeout(() => { toast.className = ''; }, 3000);
}

function switchTab(name) {
  document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(c => c.style.display = 'none');
  event.currentTarget.classList.add('active');
  document.getElementById('tab-' + name).style.display = 'block';
  if (name === 'raw') {
    document.getElementById('raw-json-text').value = JSON.stringify(state.catalog, null, 2);
  } else if (name === 'backups') {
    loadBackups();
  }
}

async function loadAllData() {
  try {
    const res = await fetch('/api/catalog');
    const data = await res.json();
    if (data.error) throw new Error(data.error);
    state.catalog = data.catalog;
    state.catalogPath = data.path;

    document.getElementById('catalog-path-badge').textContent = '路径: ' + (data.path || 'default.json');
    document.getElementById('configured-count-badge').textContent = '已启用: ' + state.catalog.models.length;
    document.getElementById('tab-configured-num').textContent = state.catalog.models.length;

    renderConfiguredTable();
    loadBackups();
    if (state.upstreamModels.length === 0) {
      fetchUpstream(true);
    } else {
      updateComparison();
    }
  } catch (err) {
    showToast('加载失败: ' + err.message, 'error');
  }
}

async function fetchUpstream(silent = false) {
  const url = document.getElementById('upstream-url').value;
  const key = document.getElementById('upstream-key').value;
  if (!silent) showToast('正在拉取上游模型源...', 'info');

  try {
    const res = await fetch('/api/models/fetch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, api_key: key })
    });
    const data = await res.json();
    if (data.error) throw new Error(data.error);
    state.upstreamModels = data.upstream_models || [];
    state.comparison = data.comparison;

    document.getElementById('upstream-count-badge').textContent = '上游可用: ' + state.upstreamModels.length;
    document.getElementById('tab-upstream-num').textContent = state.upstreamModels.length;

    renderUpstreamTable();
    if (!silent) showToast('成功拉取 ' + state.upstreamModels.length + ' 个上游模型', 'success');
  } catch (err) {
    showToast('拉取上游失败: ' + err.message, 'error');
  }
}

function updateComparison() {
  const configuredSlugs = new Set(state.catalog.models.map(m => m.slug));
  renderUpstreamTable();
}

function renderConfiguredTable() {
  const filter = (document.getElementById('configured-search').value || '').toLowerCase().trim();
  const tbody = document.getElementById('configured-tbody');
  tbody.innerHTML = '';

  const models = (state.catalog.models || []).filter(m => {
    if (!filter) return true;
    return (m.slug && m.slug.toLowerCase().includes(filter)) ||
           (m.display_name && m.display_name.toLowerCase().includes(filter)) ||
           (m.description && m.description.toLowerCase().includes(filter));
  });

  if (models.length === 0) {
    tbody.innerHTML = '<tr><td colspan="8" style="text-align: center; color: var(--text-muted); padding: 24px;">暂无匹配的配置模型</td></tr>';
    return;
  }

  models.forEach((m, idx) => {
    const tr = document.createElement('tr');
    tr.setAttribute('draggable', 'true');
    tr.dataset.slug = m.slug;
    tr.dataset.index = idx;

    tr.addEventListener('dragstart', handleDragStart);
    tr.addEventListener('dragover', handleDragOver);
    tr.addEventListener('drop', handleDrop);
    tr.addEventListener('dragend', handleDragEnd);

    const reasoning = m.default_reasoning_level || 'none';
    const shell = m.shell_type || 'shell_command';
    const vis = m.visibility || 'list';

    tr.innerHTML = `
      <td style="text-align: center; cursor: grab; user-select: none; color: var(--text-muted); font-size: 14px;" title="按住拖拽排序">☰</td>
      <td><span class="badge">#${idx + 1} (${m.priority || 1000})</span></td>
      <td>
        <div class="model-slug">${escapeHtml(m.slug)}</div>
        <div class="model-desc">${escapeHtml(m.description || '')}</div>
      </td>
      <td class="model-name">${escapeHtml(m.display_name || m.slug)}</td>
      <td>
        <span class="tag reasoning">${escapeHtml(reasoning)}</span>
      </td>
      <td><span class="tag shell">${escapeHtml(shell)}</span></td>
      <td><span class="tag">${escapeHtml(vis)}</span></td>
      <td style="text-align: right; white-space: nowrap;">
        <button class="btn sm" onclick="moveModel('${m.slug}', 'top')" title="置顶">🔝</button>
        <button class="btn sm" onclick="moveModel('${m.slug}', 'up')" title="上移">▲</button>
        <button class="btn sm" onclick="moveModel('${m.slug}', 'down')" title="下移">▼</button>
        <button class="btn sm" onclick="moveModel('${m.slug}', 'bottom')" title="置底">🔚</button>
        <button class="btn sm blue" onclick="openEditModelModal('${m.slug}')">编辑</button>
        <button class="btn sm danger" onclick="confirmDeleteModel('${m.slug}')">删除</button>
      </td>
    `;
    tbody.appendChild(tr);
  });
}

let draggedRow = null;
function handleDragStart(e) {
  draggedRow = this;
  this.style.opacity = '0.4';
  e.dataTransfer.effectAllowed = 'move';
  e.dataTransfer.setData('text/plain', this.dataset.slug);
}
function handleDragOver(e) {
  if (e.preventDefault) e.preventDefault();
  e.dataTransfer.dropEffect = 'move';
  return false;
}
function handleDrop(e) {
  if (e.stopPropagation) e.stopPropagation();
  if (draggedRow !== this) {
    const fromSlug = draggedRow.dataset.slug;
    const toSlug = this.dataset.slug;
    const models = state.catalog.models;
    const fromIdx = models.findIndex(m => m.slug === fromSlug);
    const toIdx = models.findIndex(m => m.slug === toSlug);
    if (fromIdx >= 0 && toIdx >= 0) {
      const item = models.splice(fromIdx, 1)[0];
      models.splice(toIdx, 0, item);
      saveReorderedList();
    }
  }
  return false;
}
function handleDragEnd() {
  if (draggedRow) draggedRow.style.opacity = '1';
  draggedRow = null;
}

async function saveReorderedList() {
  const slugs = state.catalog.models.map(m => m.slug);
  try {
    const res = await fetch('/api/models/reorder', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ slugs })
    });
    const data = await res.json();
    if (data.error) throw new Error(data.error);
    showToast('模型顺序已更新并保存！', 'success');
    loadAllData();
  } catch (err) {
    showToast('更新顺序失败: ' + err.message, 'error');
  }
}

async function moveModel(slug, direction) {
  try {
    const res = await fetch('/api/models/move', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ slug, direction })
    });
    const data = await res.json();
    if (data.error) throw new Error(data.error);
    showToast(`模型 '${slug}' 位置调整成功`, 'success');
    loadAllData();
  } catch (err) {
    showToast('移动失败: ' + err.message, 'error');
  }
}

function renderUpstreamTable() {
  const filter = (document.getElementById('upstream-search').value || '').toLowerCase().trim();
  const tbody = document.getElementById('upstream-tbody');
  tbody.innerHTML = '';

  const configuredMap = {};
  (state.catalog.models || []).forEach(m => { configuredMap[m.slug] = m; });

  const upModels = (state.upstreamModels || []).filter(up => {
    const slug = up.id || up.slug;
    if (!filter) return true;
    return slug && slug.toLowerCase().includes(filter);
  });

  if (upModels.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5" style="text-align: center; color: var(--text-muted); padding: 24px;">未发现可用模型源或无搜索匹配结果</td></tr>';
    return;
  }

  upModels.forEach(up => {
    const slug = up.id || up.slug;
    const isEnabled = !!configuredMap[slug];
    const cfg = configuredMap[slug];
    const tr = document.createElement('tr');

    tr.innerHTML = `
      <td>
        ${isEnabled
          ? '<span class="tag green">● 已启用</span>'
          : '<span class="tag" style="color: var(--text-muted);">○ 未启用</span>'}
      </td>
      <td><span class="model-slug">${escapeHtml(slug)}</span></td>
      <td><span class="badge">${escapeHtml(up.owned_by || '-')}</span></td>
      <td>${cfg ? escapeHtml(cfg.display_name || '-') : '<span style="color: var(--text-muted);">-</span>'}</td>
      <td style="text-align: right;">
        ${isEnabled
          ? `<button class="btn sm blue" onclick="openEditModelModal('${slug}')">编辑配置</button>`
          : `<button class="btn sm primary" onclick="quickEnableUpstream('${slug}')">+ 开启到 Codex</button>`}
      </td>
    `;
    tbody.appendChild(tr);
  });
}

function quickEnableUpstream(slug) {
  openAddModelModal(slug);
}

function openAddModelModal(prefilledSlug = '') {
  document.getElementById('modal-title').textContent = '开启新模型 (Add Model)';
  document.getElementById('edit-is-add').value = '1';
  document.getElementById('edit-orig-slug').value = '';
  document.getElementById('edit-slug').readOnly = false;

  // 填充模板下拉列表
  const templateSelect = document.getElementById('edit-template-slug');
  templateSelect.innerHTML = '';
  (state.catalog.models || []).forEach(m => {
    const opt = document.createElement('option');
    opt.value = m.slug;
    opt.textContent = `${m.slug} (${m.display_name || ''})`;
    templateSelect.appendChild(opt);
  });
  document.getElementById('template-select-row').style.display = 'flex';

  // 智能推荐模板
  let defaultTmpl = '';
  if (prefilledSlug.includes('gemini') || prefilledSlug.includes('claude')) {
    defaultTmpl = (state.catalog.models.find(m => m.slug.includes('gemini')) || state.catalog.models[0] || {}).slug || '';
  } else {
    defaultTmpl = (state.catalog.models.find(m => m.slug.includes('deepseek')) || state.catalog.models[0] || {}).slug || '';
  }
  templateSelect.value = defaultTmpl;

  document.getElementById('edit-slug').value = prefilledSlug || '';
  document.getElementById('edit-display-name').value = prefilledSlug ? formatDisplayName(prefilledSlug) : '';
  document.getElementById('edit-description').value = prefilledSlug ? `Model ${prefilledSlug}` : '';

  const maxPrio = Math.max(...(state.catalog.models.map(m => m.priority || 999)), 999);
  document.getElementById('edit-priority').value = maxPrio + 1;
  document.getElementById('edit-default-reasoning').value = 'medium';
  document.getElementById('edit-shell-type').value = 'unified_exec';
  document.getElementById('edit-visibility').value = 'list';

  onTemplateChange();
  document.getElementById('edit-modal').classList.add('open');
}

function formatDisplayName(slug) {
  return slug.split('-').map(s => s.charAt(0).toUpperCase() + s.slice(1)).join('-');
}

function onTemplateChange() {
  const tmplSlug = document.getElementById('edit-template-slug').value;
  const tmpl = state.catalog.models.find(m => m.slug === tmplSlug);
  if (!tmpl) return;

  document.getElementById('edit-shell-type').value = tmpl.shell_type || 'shell_command';
  document.getElementById('edit-default-reasoning').value = tmpl.default_reasoning_level || 'medium';
  document.getElementById('edit-context-window').value = tmpl.context_window || 1048576;
  document.getElementById('edit-max-output-tokens').value = tmpl.max_output_tokens || 131072;
  document.getElementById('edit-auto-compact').value = tmpl.auto_compact_token_limit || 943718;

  // reasoning checkboxes
  const supported = (tmpl.supported_reasoning_levels || []).map(r => r.effort);
  document.querySelectorAll('#reasoning-checkboxes input').forEach(cb => {
    cb.checked = supported.includes(cb.value);
  });
}

function openEditModelModal(slug) {
  const m = state.catalog.models.find(x => x.slug === slug);
  if (!m) return;

  document.getElementById('modal-title').textContent = '编辑模型: ' + slug;
  document.getElementById('edit-is-add').value = '0';
  document.getElementById('edit-orig-slug').value = slug;
  document.getElementById('template-select-row').style.display = 'none';

  document.getElementById('edit-slug').value = m.slug;
  document.getElementById('edit-slug').readOnly = false;
  document.getElementById('edit-display-name').value = m.display_name || '';
  document.getElementById('edit-description').value = m.description || '';
  document.getElementById('edit-priority').value = m.priority || 1000;
  document.getElementById('edit-default-reasoning').value = m.default_reasoning_level || 'medium';
  document.getElementById('edit-shell-type').value = m.shell_type || 'shell_command';
  document.getElementById('edit-visibility').value = m.visibility || 'list';

  document.getElementById('edit-context-window').value = m.context_window || 1048576;
  document.getElementById('edit-max-output-tokens').value = m.max_output_tokens || 131072;
  document.getElementById('edit-auto-compact').value = m.auto_compact_token_limit || 943718;

  const supported = (m.supported_reasoning_levels || []).map(r => r.effort);
  document.querySelectorAll('#reasoning-checkboxes input').forEach(cb => {
    cb.checked = supported.includes(cb.value);
  });

  document.getElementById('edit-modal').classList.add('open');
}

function closeEditModal() {
  document.getElementById('edit-modal').classList.remove('open');
}

async function submitEditForm() {
  const isAdd = document.getElementById('edit-is-add').value === '1';
  const origSlug = document.getElementById('edit-orig-slug').value;
  const slug = document.getElementById('edit-slug').value.trim();
  const displayName = document.getElementById('edit-display-name').value.trim();
  const desc = document.getElementById('edit-description').value.trim();
  const priority = parseInt(document.getElementById('edit-priority').value, 10) || 1000;
  const defaultReasoning = document.getElementById('edit-default-reasoning').value;
  const shellType = document.getElementById('edit-shell-type').value;
  const visibility = document.getElementById('edit-visibility').value;
  const contextWindow = parseInt(document.getElementById('edit-context-window').value, 10) || 1048576;
  const maxOutputTokens = parseInt(document.getElementById('edit-max-output-tokens').value, 10) || 131072;
  const autoCompact = parseInt(document.getElementById('edit-auto-compact').value, 10) || 943718;

  if (!slug) {
    showToast('Slug 不能为空', 'error');
    return;
  }

  const supportedReasoning = [];
  document.querySelectorAll('#reasoning-checkboxes input:checked').forEach(cb => {
    supportedReasoning.push({ effort: cb.value, description: `${cb.value} reasoning effort` });
  });

  const modelConfig = {
    slug,
    display_name: displayName || slug,
    description: desc,
    priority,
    default_reasoning_level: defaultReasoning,
    supported_reasoning_levels: supportedReasoning,
    shell_type: shellType,
    visibility,
    context_window: contextWindow,
    max_context_window: contextWindow,
    max_output_tokens: maxOutputTokens,
    auto_compact_token_limit: autoCompact
  };

  try {
    if (isAdd) {
      const tmplSlug = document.getElementById('edit-template-slug').value;
      const res = await fetch('/api/models/add', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ config: modelConfig, template_slug: tmplSlug })
      });
      const data = await res.json();
      if (data.error) throw new Error(data.error);
      showToast(`模型 '${slug}' 开启并添加成功！`, 'success');
    } else {
      const res = await fetch('/api/models/update', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ slug: origSlug, updates: modelConfig })
      });
      const data = await res.json();
      if (data.error) throw new Error(data.error);
      showToast(`模型 '${slug}' 更新成功！`, 'success');
    }
    closeEditModal();
    loadAllData();
  } catch (err) {
    showToast('操作失败: ' + err.message, 'error');
  }
}

async function confirmDeleteModel(slug) {
  if (!confirm(`确定要从 Codex 模型目录中删除模型 '${slug}' 吗？\n删除前系统会自动创建完整安全备份。`)) return;
  try {
    const res = await fetch('/api/models/remove', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ slug })
    });
    const data = await res.json();
    if (data.error) throw new Error(data.error);
    showToast(`模型 '${slug}' 已删除`, 'success');
    loadAllData();
  } catch (err) {
    showToast('删除失败: ' + err.message, 'error');
  }
}

async function movePriority(slug, direction) {
  const models = state.catalog.models;
  const idx = models.findIndex(m => m.slug === slug);
  if (idx < 0) return;
  const targetIdx = idx + direction;
  if (targetIdx < 0 || targetIdx >= models.length) return;

  const temp = models[idx];
  models[idx] = models[targetIdx];
  models[targetIdx] = temp;

  const slugs = models.map(m => m.slug);
  try {
    const res = await fetch('/api/models/reorder', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ slugs })
    });
    const data = await res.json();
    if (data.error) throw new Error(data.error);
    loadAllData();
  } catch (err) {
    showToast('排序失败: ' + err.message, 'error');
  }
}

function formatRawJson() {
  const textarea = document.getElementById('raw-json-text');
  try {
    const parsed = JSON.parse(textarea.value);
    textarea.value = JSON.stringify(parsed, null, 2);
  } catch (e) {
    showToast('JSON 格式有误: ' + e.message, 'error');
  }
}

async function saveRawJson() {
  const textarea = document.getElementById('raw-json-text');
  let parsed;
  try {
    parsed = JSON.parse(textarea.value);
  } catch (e) {
    showToast('无效的 JSON: ' + e.message, 'error');
    return;
  }
  if (!parsed.models || !Array.isArray(parsed.models)) {
    showToast('JSON 必须包含 models 数组', 'error');
    return;
  }

  try {
    const res = await fetch('/api/catalog/raw-save', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ catalog: parsed })
    });
    const data = await res.json();
    if (data.error) throw new Error(data.error);
    showToast('原始 JSON 已保存并成功应用！', 'success');
    loadAllData();
  } catch (err) {
    showToast('保存失败: ' + err.message, 'error');
  }
}

async function loadBackups() {
  try {
    const res = await fetch('/api/backups');
    const data = await res.json();
    state.backups = data.backups || [];
    document.getElementById('tab-backups-num').textContent = state.backups.length;
    renderBackupsTable();
  } catch (err) {
    console.error('加载备份失败', err);
  }
}

function renderBackupsTable() {
  const tbody = document.getElementById('backups-tbody');
  tbody.innerHTML = '';
  if (!state.backups || state.backups.length === 0) {
    tbody.innerHTML = '<tr><td colspan="4" style="text-align: center; color: var(--text-muted); padding: 20px;">暂无备份记录</td></tr>';
    return;
  }
  state.backups.forEach(b => {
    const tr = document.createElement('tr');
    const szKb = (b.size / 1024).toFixed(1) + ' KB';
    tr.innerHTML = `
      <td>${escapeHtml(b.time_str)}</td>
      <td><span class="badge">${szKb}</span></td>
      <td><span class="model-slug" style="font-size: 11px;">${escapeHtml(b.filename)}</span></td>
      <td style="text-align: right;">
        <button class="btn sm blue" onclick="restoreBackup('${escapeHtml(b.filename)}')">还原此版本</button>
      </td>
    `;
    tbody.appendChild(tr);
  });
}

async function restoreBackup(filename) {
  if (!confirm(`确定要将模型配置还原到备份版本 [${filename}] 吗？\n当前配置将被新备份覆盖保护。`)) return;
  try {
    const res = await fetch('/api/backups/restore', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ filename })
    });
    const data = await res.json();
    if (data.error) throw new Error(data.error);
    showToast('还原备份成功！', 'success');
    loadAllData();
  } catch (err) {
    showToast('还原失败: ' + err.message, 'error');
  }
}

async function createManualBackup() {
  try {
    const res = await fetch('/api/backups/create', { method: 'POST' });
    const data = await res.json();
    if (data.error) throw new Error(data.error);
    showToast('备份已创建！', 'success');
    loadBackups();
  } catch (err) {
    showToast('创建备份失败: ' + err.message, 'error');
  }
}

function escapeHtml(str) {
  if (!str) return '';
  return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

// 初始化加载
loadAllData();
</script>
</body>
</html>
'''


class CatalogRequestHandler(http.server.BaseHTTPRequestHandler):
    catalog_path = None

    def log_message(self, format, *args):
        # 静默常规 access 日志，只输出关键信息
        pass

    def send_json(self, status: int, data: Dict[str, Any]):
        payload = json.dumps(data, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization")
        self.end_headers()
        self.wfile.write(payload)

    def do_OPTIONS(self):
        self.send_response(200)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization")
        self.end_headers()

    def read_body_json(self) -> Dict[str, Any]:
        length = int(self.headers.get("Content-Length", 0))
        if length <= 0:
            return {}
        raw = self.rfile.read(length).decode("utf-8")
        return json.loads(raw)

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path

        if path == "/" or path == "/index.html":
            html_bytes = HTML_TEMPLATE.encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(html_bytes)))
            self.end_headers()
            self.wfile.write(html_bytes)
            return

        if path == "/api/catalog":
            try:
                cat = core.load_catalog(self.catalog_path)
                self.send_json(200, {
                    "path": core.get_catalog_path(self.catalog_path),
                    "catalog": cat
                })
            except Exception as e:
                self.send_json(500, {"error": str(e)})
            return

        if path == "/api/backups":
            try:
                backups = core.list_backups()
                self.send_json(200, {"backups": backups})
            except Exception as e:
                self.send_json(500, {"error": str(e)})
            return

        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path

        if path == "/api/models/fetch":
            try:
                body = self.read_body_json()
                url = body.get("url") or core.DEFAULT_UPSTREAM_URL
                api_key = body.get("api_key") or core.DEFAULT_API_KEY
                upstream_models = core.fetch_upstream_models(url, api_key)
                cat = core.load_catalog(self.catalog_path)
                comp = core.compare_models(cat, upstream_models)
                self.send_json(200, {
                    "upstream_models": upstream_models,
                    "comparison": comp
                })
            except Exception as e:
                self.send_json(500, {"error": str(e)})
            return

        if path == "/api/models/add":
            try:
                body = self.read_body_json()
                config = body.get("config", {})
                tmpl = body.get("template_slug")
                cat = core.load_catalog(self.catalog_path)
                added = core.add_model(cat, config, template_slug=tmpl)
                saved = core.save_catalog(cat, self.catalog_path)
                self.send_json(200, {"success": True, "added": added, "path": saved})
            except Exception as e:
                self.send_json(400, {"error": str(e)})
            return

        if path == "/api/models/reorder":
            try:
                body = self.read_body_json()
                slugs = body.get("slugs", [])
                cat = core.load_catalog(self.catalog_path)
                core.reorder_models(cat, slugs)
                saved = core.save_catalog(cat, self.catalog_path)
                self.send_json(200, {"success": True, "path": saved})
            except Exception as e:
                self.send_json(400, {"error": str(e)})
            return

        if path == "/api/models/move":
            try:
                body = self.read_body_json()
                slug = body.get("slug")
                direction = body.get("direction")
                target_index = body.get("target_index")
                cat = core.load_catalog(self.catalog_path)
                new_idx = core.move_model(cat, slug, direction=direction, target_index=target_index)
                saved = core.save_catalog(cat, self.catalog_path)
                self.send_json(200, {"success": True, "new_index": new_idx, "path": saved})
            except Exception as e:
                self.send_json(400, {"error": str(e)})
            return

        if path == "/api/catalog/raw-save":
            try:
                body = self.read_body_json()
                catalog_data = body.get("catalog", {})
                saved = core.save_catalog(catalog_data, self.catalog_path)
                self.send_json(200, {"success": True, "path": saved})
            except Exception as e:
                self.send_json(400, {"error": str(e)})
            return

        if path == "/api/backups/create":
            try:
                backup_path = core.create_backup(self.catalog_path)
                self.send_json(200, {"success": True, "path": backup_path})
            except Exception as e:
                self.send_json(500, {"error": str(e)})
            return

        if path == "/api/backups/restore":
            try:
                body = self.read_body_json()
                filename = body.get("filename")
                if not filename:
                    raise ValueError("Missing backup filename")
                restored = core.restore_backup(filename, self.catalog_path)
                self.send_json(200, {"success": True, "path": restored})
            except Exception as e:
                self.send_json(500, {"error": str(e)})
            return

        self.send_response(404)
        self.end_headers()

    def do_PUT(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path

        if path == "/api/models/update":
            try:
                body = self.read_body_json()
                slug = body.get("slug")
                updates = body.get("updates", {})
                if not slug:
                    raise ValueError("Missing model slug")
                cat = core.load_catalog(self.catalog_path)
                updated = core.update_model(cat, slug, updates)
                saved = core.save_catalog(cat, self.catalog_path)
                self.send_json(200, {"success": True, "updated": updated, "path": saved})
            except Exception as e:
                self.send_json(400, {"error": str(e)})
            return

        self.send_response(404)
        self.end_headers()

    def do_DELETE(self):
        parsed = urllib.parse.urlparse(self.path)
        path = parsed.path

        if path == "/api/models/remove":
            try:
                body = self.read_body_json()
                slug = body.get("slug")
                if not slug:
                    raise ValueError("Missing model slug")
                cat = core.load_catalog(self.catalog_path)
                removed = core.remove_model(cat, slug)
                saved = core.save_catalog(cat, self.catalog_path)
                self.send_json(200, {"success": True, "removed": removed, "path": saved})
            except Exception as e:
                self.send_json(400, {"error": str(e)})
            return

        self.send_response(404)
        self.end_headers()


def run_server(host: str = "127.0.0.1", port: int = 8388, catalog: str = None, open_browser: bool = True):
    CatalogRequestHandler.catalog_path = catalog

    # 尝试绑定端口，若被占用尝试递增端口
    current_port = port
    server = None
    for p in range(current_port, current_port + 20):
        try:
            server = socketserver.TCPServer((host, p), CatalogRequestHandler)
            current_port = p
            break
        except OSError:
            continue

    if not server:
        print(f"[Error] Could not bind to {host} on ports {port}-{port+20}", file=sys.stderr)
        sys.exit(1)

    url = f"http://{host}:{current_port}"
    print("=" * 60)
    print(f" Codex Model Catalog Visual Web Editor")
    print(f" Local URL : {url}")
    print(f" Catalog   : {core.get_catalog_path(catalog)}")
    print("=" * 60)
    print("Press Ctrl+C to stop the server.")

    if open_browser:
        threading.Timer(0.8, lambda: webbrowser.open(url)).start()

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nStopping web editor server...")
    finally:
        server.server_close()


def main():
    parser = argparse.ArgumentParser(description="Codex Model Catalog Visual Web Editor")
    parser.add_argument("--host", default="127.0.0.1", help="Binding host (default: 127.0.0.1)")
    parser.add_argument("--port", "-p", type=int, default=8388, help="Port (default: 8388)")
    parser.add_argument("--catalog", "-c", help="Path to default.json")
    parser.add_argument("--no-browser", action="store_true", help="Do not automatically open browser")

    args = parser.parse_args()
    run_server(host=args.host, port=args.port, catalog=args.catalog, open_browser=not args.no_browser)


if __name__ == "__main__":
    main()
