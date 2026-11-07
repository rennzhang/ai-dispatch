function desktopModelKey(value) {
  return value.toLowerCase().replace(/[^a-z0-9]/g, '');
}

function desktopPreset(label) {
  const fast = /\bfast\b|快速|加速/i.test(label);
  const plain = label.replace(/\bfast\b|快速|加速/gi, '').trim();
  const suffix = plain.match(/\s*(extra[ -]?high|xhigh|ultra[ -]?high|minimal|medium|high|low|none|max|超深度|超高|极高|最高|深度|轻度|标准|高|中|低|无)\s*$/i);
  const levels = { 'extra high': 'xhigh', 'extra-high': 'xhigh', 'extrahigh': 'xhigh', 'ultra high': 'xhigh', 'ultra-high': 'xhigh', '超深度': 'xhigh', '超高': 'xhigh', '极高': 'xhigh', '最高': 'xhigh', '深度': 'high', '轻度': 'low', '标准': 'medium', '高': 'high', '中': 'medium', '低': 'low', '无': 'none' };
  return {
    label,
    model: desktopModelKey(suffix ? plain.slice(0, suffix.index) : plain),
    effort: suffix ? (levels[suffix[1].toLowerCase()] || suffix[1].toLowerCase()) : '',
    fast
  };
}

function desktopAX(request) {
  ObjC.import('Foundation'); ObjC.import('AppKit'); ObjC.import('ApplicationServices');
  ObjC.bindFunction('AXUIElementCopyAttributeValue', ['int', ['void *', 'id', 'id *']]);
  ObjC.bindFunction('AXUIElementGetPid', ['int', ['void *', 'int *']]);
  ObjC.bindFunction('AXUIElementPerformAction', ['int', ['void *', 'id']]);
  ObjC.bindFunction('CFEqual', ['bool', ['void *', 'void *']]);
  const running = $.NSRunningApplication.runningApplicationWithProcessIdentifier(request.pid);
  if (!running || ObjC.unwrap(running.bundleIdentifier) !== 'com.openai.codex' ||
      (request.bundle && ObjC.unwrap(running.bundleURL.path) !== request.bundle)) {
    throw Error('The requested Codex Desktop instance changed; no input was queued');
  }
  return { request, app: ObjC.castRefToObject($.AXUIElementCreateApplication(request.pid)), started: Date.now(), stage: 'initialWindow', deadline: Date.now() + 15000, changes: 0 };
}

function axAttr(element, name) {
  if (!element) return null;
  const value = Ref();
  return $.AXUIElementCopyAttributeValue(ObjC.castObjectToRef(element), $(name), value) === 0 ? value[0] : null;
}

function axText(element, name) {
  const value = axAttr(element, name);
  return value === null ? '' : String(ObjC.unwrap(value));
}

function axLabel(element) {
  return axText(element, 'AXTitle') || axText(element, 'AXDescription') || axText(element, 'AXValue');
}

function axWindow(ctx) {
  const windows = axAttr(ctx.app, 'AXWindows');
  const listed = windows ? windows.js : [];
  // Chromium can temporarily omit a live window from AXWindows while its
  // settings menu is open. These are candidates in the same native AX graph.
  const candidates = [axAttr(ctx.app, 'AXFocusedWindow'), axAttr(ctx.app, 'AXMainWindow')].concat(listed);
  const matches = [];
  for (const window of candidates) {
    if (!window || axText(window, 'AXTitle') !== ctx.request.title) continue;
    const pid = Ref();
    if ($.AXUIElementGetPid(ObjC.castObjectToRef(window), pid) !== 0 || pid[0] !== ctx.request.pid) continue;
    if (!matches.some(existing => $.CFEqual(ObjC.castObjectToRef(existing), ObjC.castObjectToRef(window)))) matches.push(window);
  }
  if (matches.length !== 1) throw Error('The exact requested Desktop thread window is not accessible (' + axWindowState(ctx, matches.length, listed.length) + '); no input was queued');
  return matches[0];
}

function axWindowState(ctx, matches, count) {
  const state = { pid: ctx.request.pid, matches, windows: count, stage: ctx.stage, elapsed_ms: Date.now() - ctx.started };
  for (const name of ['AXFocusedWindow', 'AXMainWindow']) {
    const window = axAttr(ctx.app, name), pid = Ref();
    state[name] = { present: Boolean(window), matches: window ? axText(window, 'AXTitle') === ctx.request.title : false };
    if (window && $.AXUIElementGetPid(ObjC.castObjectToRef(window), pid) === 0) state[name].pid = pid[0];
  }
  return JSON.stringify(state);
}

function axFind(ctx, predicate, root) {
  const pending = [root || axWindow(ctx)];
  let visited = 0;
  while (pending.length && visited++ < 4096 && Date.now() <= ctx.deadline) {
    const element = pending.pop();
    if (predicate(element, axText(element, 'AXRole'))) return element;
    const children = axAttr(element, 'AXChildren');
    if (children) pending.push.apply(pending, children.js);
  }
  return null;
}

function axWait(ctx, reader) {
  let last;
  while (Date.now() <= ctx.deadline) {
    try { return reader(); } catch (error) {
      last = error;
      delay(0.05);
    }
  }
  throw last || Error('Timed out waiting for Desktop settings; no input was queued');
}

function axAction(ctx, element, action) {
  axWindow(ctx);
  if (!element) throw Error('The requested Desktop settings control is unavailable; no input was queued');
  const status = $.AXUIElementPerformAction(ObjC.castObjectToRef(element), $(action));
  if (status !== 0) throw Error('Desktop settings action ' + action + ' failed (' + status + '); no input was queued');
  ctx.changes++;
  delay(0.05);
}

function modelPopup(ctx) {
  const popup = axFind(ctx, (element, role) => role === 'AXPopUpButton' && /^(GPT[- ]|o[134](?:\b|-)|选择推理强度$|Choose reasoning strength$)/i.test(axLabel(element)));
  if (!popup) throw Error('The bound Desktop thread has no accessible model selector; no input was queued');
  return popup;
}

function findStrengthMenu(ctx) {
  return axFind(ctx, (element, role) => role === 'AXGroup' && /^(选择推理强度|Choose reasoning strength)$/i.test(axLabel(element)));
}

function requireStrengthMenu(ctx) {
  const menu = findStrengthMenu(ctx);
  if (!menu) throw Error('The opened Desktop reasoning menu is not accessible; no input was queued');
  return menu;
}

function strengthMenu(ctx) {
  let menu = findStrengthMenu(ctx);
  if (!menu) {
    axAction(ctx, modelPopup(ctx), 'AXPress');
    menu = axWait(ctx, () => requireStrengthMenu(ctx));
  }
  return menu;
}

function menuItem(ctx, menu, pattern) {
  const item = axFind(ctx, (element, role) => role === 'AXMenuItem' && pattern.test(axLabel(element)), menu);
  if (!item) throw Error('The requested Desktop menu control is unavailable; no input was queued');
  return item;
}

function strengthState(ctx, menu) {
  const item = axFind(ctx, (element, role) => role === 'AXStaticText' && /(?:第\s*\d+\s*项|\bitem\s+\d+)/i.test(axLabel(element)), menu);
  const label = item ? axLabel(item) : '';
  const position = label.match(/^(.*?)[,，]\s*第\s*(\d+)\s*项\s*[,，]\s*共\s*(\d+)\s*项/) || label.match(/^(.*?)[,，]\s*item\s+(\d+)\s+(?:of|\/)\s*(\d+)/i);
  if (!position || +position[3] < 1 || +position[3] > 32) throw Error('The Desktop strength control has no readable bounded levels; no input was queued');
  const preset = desktopPreset(position[1]);
  preset.index = +position[2]; preset.count = +position[3];
  return preset;
}

function closeStrengthMenu(ctx) {
  const expanded = axAttr(modelPopup(ctx), 'AXExpanded');
  if (expanded === null) throw Error('The Desktop model selector has no readable expanded state; no input was queued');
  if (ObjC.unwrap(expanded) === true) postSettingsKey(ctx, 53);
  axWait(ctx, () => {
    const popup = modelPopup(ctx), menu = findStrengthMenu(ctx);
    const visibleItem = menu && axFind(ctx, (element, role) => role === 'AXMenuItem' && ObjC.unwrap(axAttr(element, 'AXHidden')) !== true, menu);
    if (ObjC.unwrap(axAttr(popup, 'AXExpanded')) !== false || !desktopPreset(axLabel(popup)).model || visibleItem) {
      throw Error('The Desktop settings menu did not close with a readable model label and no visible items; no input was queued');
    }
    return true;
  });
}

function chooseModel(ctx, model) {
  if (!model || desktopPreset(axLabel(modelPopup(ctx))).model === desktopModelKey(model)) return;
  const menu = strengthMenu(ctx);
  axAction(ctx, menuItem(ctx, menu, /^(选择模型|Select model|Choose model)$/i), 'AXPress');
  const choice = axWait(ctx, () => {
    const item = axFind(ctx, (element, role) => role === 'AXMenuItem' && desktopModelKey(axLabel(element)) === desktopModelKey(model));
    if (!item) throw Error('The requested model is not available in the Desktop model menu; no input was queued');
    return item;
  });
  axAction(ctx, choice, 'AXPress');
  axWait(ctx, () => { if (desktopPreset(axLabel(modelPopup(ctx))).model !== desktopModelKey(model)) throw Error('The Desktop model selection was not committed; no input was queued'); return true; });
}

function postSettingsKey(ctx, key) {
  const window = axWindow(ctx), focused = axAttr(ctx.app, 'AXFocusedWindow'), pid = Ref();
  if (!focused || axText(focused, 'AXTitle') !== ctx.request.title ||
      $.AXUIElementGetPid(ObjC.castObjectToRef(focused), pid) !== 0 || pid[0] !== ctx.request.pid ||
      !$.CFEqual(ObjC.castObjectToRef(window), ObjC.castObjectToRef(focused))) {
    throw Error('The Desktop keyboard target is not the verified focused thread window; no key or input was sent');
  }
  const down = $.CGEventCreateKeyboardEvent(null, key, true);
  const up = $.CGEventCreateKeyboardEvent(null, key, false);
  $.CGEventSetFlags(down, 0); $.CGEventSetFlags(up, 0);
  $.CGEventPostToPid(ctx.request.pid, down); $.CGEventPostToPid(ctx.request.pid, up);
  ctx.changes++;
}

function strengthKey(ctx, key) {
  ctx.stage = 'chooseEffort.focusControl';
  axAction(ctx, menuItem(ctx, requireStrengthMenu(ctx), /^(强度|Strength)$/i), 'AXPress');
  ctx.stage = 'chooseEffort.sendArrow';
  postSettingsKey(ctx, key);
}

function chooseEffort(ctx, requested) {
  if (!requested || requested === 'auto') return;
  strengthMenu(ctx);
  let current = axWait(ctx, () => strengthState(ctx, requireStrengthMenu(ctx)));
  const model = current.model, count = current.count;
  const levels = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'];
  const wanted = levels.indexOf(requested), seen = new Set();
  for (let step = 0; step < count; step++) {
    if (current.effort === requested) return;
    if (current.model !== model || wanted < 0 || levels.indexOf(current.effort) < 0 || seen.has(current.label)) break;
    seen.add(current.label);
    const previous = current;
    strengthKey(ctx, levels.indexOf(current.effort) < wanted ? 124 : 123);
    ctx.stage = 'chooseEffort.readChangedLevel';
    current = axWait(ctx, () => {
      const next = strengthState(ctx, requireStrengthMenu(ctx));
      if (next.index === previous.index || next.label === previous.label) throw Error('The Desktop strength key did not change its readable level; no input was queued');
      return next;
    });
  }
  throw Error('The requested effort is not available in the Desktop strength menu; no input was queued');
}

function dismissModelMenus(ctx) {
  closeStrengthMenu(ctx);
  const choice = axFind(ctx, (element, role) => role === 'AXMenuItem' && /^(GPT[- ]6(?:[. -]1)?[- ](?:Sol|Terra|Astra))$/i.test(axLabel(element)));
  if (choice) axAction(ctx, modelPopup(ctx), 'AXPress');
}

function chooseSpeed(ctx, fast) {
  strengthMenu(ctx);
  const read = () => {
    const item = menuItem(ctx, requireStrengthMenu(ctx), /(?:快速模式|Fast mode)/i);
    const value = axText(item, 'AXValue');
    if (!/^(0|1|true|false)$/i.test(value)) throw Error('The Desktop speed control has no readable state; no input was queued');
    return { item, fast: /^(1|true)$/i.test(value) };
  };
  const before = axWait(ctx, read);
  if (before.fast !== fast) axAction(ctx, before.item, 'AXPress');
  axWait(ctx, () => { if (read().fast !== fast) throw Error('The Desktop speed selection was not committed; no input was queued'); return true; });
}

function configurePreset(ctx, model, effort, fast) {
  ctx.stage = 'chooseModel';
  chooseModel(ctx, model);
  ctx.stage = 'chooseEffort';
  chooseEffort(ctx, effort);
  ctx.stage = 'chooseSpeed';
  chooseSpeed(ctx, fast);
  ctx.stage = 'closeMenu';
  closeStrengthMenu(ctx);
  ctx.stage = 'readCommittedPreset';
  return axWait(ctx, () => {
    const actual = desktopPreset(axLabel(modelPopup(ctx)));
    if ((model && actual.model !== desktopModelKey(model)) || (effort && effort !== 'auto' && actual.effort !== effort) || actual.fast !== fast) {
      throw Error('The committed Desktop model, effort or speed does not match the request; no input was queued');
    }
    return actual;
  });
}

function run(argv) {
  const request = JSON.parse(argv[0]);
  const ctx = desktopAX(request);
  axWait(ctx, () => axWindow(ctx));
  let initial = axWait(ctx, () => desktopPreset(axLabel(modelPopup(ctx))));
  if (!initial.model) {
    dismissModelMenus(ctx);
    initial = axWait(ctx, () => { const preset = desktopPreset(axLabel(modelPopup(ctx))); if (!preset.model) throw Error('The Desktop selector has no committed model label; no input was queued'); return preset; });
  }
  if ((!request.model || initial.model === desktopModelKey(request.model)) &&
      (!request.effort || request.effort === 'auto' || initial.effort === request.effort) && initial.fast === request.fast) {
    return JSON.stringify({ pid: request.pid, title: request.title, preset: initial.label });
  }
  try {
    const actual = configurePreset(ctx, request.model, request.effort, request.fast);
    return JSON.stringify({ pid: request.pid, title: request.title, preset: actual.label });
  } catch (error) {
    const first = 'stage=' + ctx.stage + ' elapsed_ms=' + (Date.now() - ctx.started) + ': ' + String(error);
    if (ctx.changes) {
      ctx.deadline = Date.now() + 5000;
      try { closeStrengthMenu(ctx); configurePreset(ctx, initial.model, initial.effort, initial.fast); }
      catch (restore) { throw Error(first + '; the original Desktop preset could not be restored: ' + String(restore)); }
    }
    throw Error(first);
  }
}
