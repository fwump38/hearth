/* Filter builder for saved views. */
(function () {
  'use strict';
  var fields = JSON.parse(document.getElementById('schema-json').textContent);
  var byKey = {}; fields.forEach(function (f) { byKey[f.key] = f; });
  var filter = {};
  try { filter = JSON.parse(document.getElementById('filter-json').value) || {}; } catch (e) {}
  filter.conds = filter.conds || [];
  var box = document.getElementById('conds');
  var logic = document.getElementById('logic');
  logic.value = filter.logic === 'or' ? 'or' : 'and';
  if (filter.groups && filter.groups.length) document.getElementById('groups-note').hidden = false;

  var OPS = {
    enum: [['is', 'is'], ['isnot', 'is not'], ['in', 'is one of'], ['contains', 'contains'], ['notcontains', 'does not contain'], ['empty', 'is empty'], ['notempty', 'is not empty']],
    text: [['contains', 'contains'], ['notcontains', 'does not contain'], ['is', 'is'], ['isnot', 'is not'], ['empty', 'is empty'], ['notempty', 'is not empty']],
    list: [['contains', 'has'], ['notcontains', 'does not have'], ['empty', 'has none'], ['notempty', 'has any']],
    bool: [['is', 'is']],
    number: [['is', '='], ['isnot', '≠'], ['gte', '≥'], ['lte', '≤'], ['empty', 'is empty']],
    date: [['within_days', 'is within the last (days)'], ['gte', 'on or after'], ['lte', 'on or before'], ['empty', 'is empty']],
  };
  function kind(f) { return f.type === 'duration' ? 'number' : f.type === 'url' ? 'text' : f.type; }
  function el(tag, attrs, kids) {
    var e = document.createElement(tag);
    Object.keys(attrs || {}).forEach(function (k) { if (k === 'text') e.textContent = attrs[k]; else e.setAttribute(k, attrs[k]); });
    (kids || []).forEach(function (c) { e.appendChild(c); });
    return e;
  }
  function select(opts, cur, cls) {
    var s = el('select', { 'class': 'form-select form-select-sm ' + (cls || '') });
    opts.forEach(function (o) { var op = el('option', { value: o[0], text: o[1] }); if (o[0] === cur) op.selected = true; s.appendChild(op); });
    return s;
  }
  function row(c) {
    var wrap = el('div', { 'class': 'd-flex flex-wrap gap-2 align-items-center cond-row' });
    var fsel = select(fields.filter(function (f) { return f.filter; }).map(function (f) { return [f.key, f.label]; }), c.field, 'w-auto');
    var osel = el('select', { 'class': 'form-select form-select-sm w-auto' });
    var val = el('span', { 'class': 'd-inline-flex' });
    var rm = el('button', { type: 'button', 'class': 'btn btn-sm btn-outline-secondary', 'aria-label': 'Remove', text: '✕' });
    wrap.appendChild(fsel); wrap.appendChild(osel); wrap.appendChild(val); wrap.appendChild(rm);

    function curVal() { return c.list && c.list.length ? c.list.join(', ') : (c.value || ''); }
    function buildVal() {
      var f = byKey[c.field] || { type: 'text' }, k = kind(f);
      val.innerHTML = '';
      if (c.op === 'empty' || c.op === 'notempty') { c.value = ''; c.list = null; return; }
      var input;
      if (k === 'bool') {
        input = select([['true', 'true'], ['false', 'false']], c.value || 'true');
        if (!c.value) c.value = 'true';
        input.addEventListener('change', function () { c.value = input.value; sync(); });
      } else if (k === 'enum' && (c.op === 'is' || c.op === 'isnot')) {
        input = select((f.values || []).map(function (v) { return [v, v]; }), c.value || (f.values || [])[0]);
        if (!c.value) c.value = (f.values || [])[0] || '';
        input.addEventListener('change', function () { c.value = input.value; sync(); });
      } else {
        input = el('input', { 'class': 'form-control form-control-sm', type: (k === 'number' || c.op === 'within_days') ? 'number' : (k === 'date' ? 'date' : 'text'), value: curVal() });
        if (k === 'list') input.setAttribute('list', 'tags-dl');
        if (k === 'enum') { var dl = 'dl-' + f.key; if (!document.getElementById(dl)) { var d = el('datalist', { id: dl }); (f.values || []).forEach(function (v) { d.appendChild(el('option', { value: v })); }); document.body.appendChild(d); } input.setAttribute('list', dl); }
        input.addEventListener('input', function () {
          if (c.op === 'in') { c.list = input.value.split(',').map(function (x) { return x.trim(); }).filter(Boolean); c.value = ''; }
          else { c.value = input.value; c.list = null; }
          sync();
        });
      }
      val.appendChild(input);
    }
    function buildOps() {
      var f = byKey[c.field] || { type: 'text' }, ops = OPS[kind(f)] || OPS.text;
      if (!ops.some(function (o) { return o[0] === c.op; })) c.op = ops[0][0];
      osel.innerHTML = '';
      ops.forEach(function (o) { var op = el('option', { value: o[0], text: o[1] }); if (o[0] === c.op) op.selected = true; osel.appendChild(op); });
    }
    fsel.addEventListener('change', function () { c.field = fsel.value; c.value = ''; c.list = null; buildOps(); buildVal(); sync(); });
    osel.addEventListener('change', function () { c.op = osel.value; buildVal(); sync(); });
    rm.addEventListener('click', function () { filter.conds.splice(filter.conds.indexOf(c), 1); render(); sync(); });
    buildOps(); buildVal();
    return wrap;
  }
  function render() {
    box.innerHTML = '';
    if (!filter.conds.length) box.appendChild(el('div', { 'class': 'text-body-secondary small', text: 'No conditions: all recipes match.' }));
    filter.conds.forEach(function (c) { box.appendChild(row(c)); });
  }
  function sync() {
    var out = { logic: logic.value };
    if (filter.conds.length) out.conds = filter.conds.map(function (c) { var o = { field: c.field, op: c.op }; if (c.value) o.value = c.value; if (c.list && c.list.length) o.list = c.list; return o; });
    if (filter.groups && filter.groups.length) out.groups = filter.groups;
    document.getElementById('filter-json').value = JSON.stringify(out);
  }
  document.getElementById('add-cond').addEventListener('click', function () {
    var first = fields.filter(function (f) { return f.filter; })[0];
    filter.conds.push({ field: first.key, op: 'is', value: '' }); render(); sync();
  });
  logic.addEventListener('change', sync);
  render(); sync();
})();
