// Progressive enhancements; every page works without them.
(() => {
  const $ = (s, root = document) => root.querySelector(s);
  const $$ = (s, root = document) => [...root.querySelectorAll(s)];
  const csrf = () => { try { return JSON.parse(document.body.getAttribute('hx-headers'))['X-CSRF-Token']; } catch { return ''; } };
  const typing = (el) => el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName));

  // Toast with an optional action (undo).
  let toastTimer;
  const toast = (text, action, onAction) => {
    const t = $('#toast');
    if (!t) return;
    t.replaceChildren(document.createTextNode(text));
    if (action) {
      const b = document.createElement('button');
      b.type = 'button';
      b.textContent = action;
      b.addEventListener('click', () => { t.hidden = true; onAction(); });
      t.append(b);
    }
    t.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { t.hidden = true; }, 5000);
  };

  // Results stick their group headings under the sticky search form.
  const form = $('.search-form');
  if (form && window.ResizeObserver && getComputedStyle(form).position === 'sticky') {
    new ResizeObserver(() => document.documentElement.style.setProperty('--stick', form.offsetHeight + 'px')).observe(form);
  }

  // Compact / full result view: flip in place and remember (cookie pw_view,
  // also read by the server so the first paint is right).
  document.addEventListener('click', (e) => {
    const a = e.target.closest('[data-view-toggle]');
    if (!a) return;
    e.preventDefault();
    const html = document.documentElement;
    const mode = html.dataset.view === 'compact' ? 'full' : 'compact';
    html.dataset.view = mode;
    document.cookie = `pw_view=${mode}; path=/; max-age=31536000; samesite=lax`;
    $$('[data-view-toggle]').forEach((t) => {
      const [c, f] = t.querySelectorAll('span');
      c.classList.toggle('on', mode === 'compact');
      f.classList.toggle('on', mode === 'full');
    });
  });

  // The word panel: a column on wide screens, a bottom sheet otherwise.
  const panel = $('#panel');
  const backdrop = $('.panel-backdrop');
  let opener = null;
  const isSheet = () => panel && getComputedStyle(panel).position === 'fixed';
  const modal = () => isSheet() && matchMedia('(max-width: 999px)').matches; // the desk's corner card is not modal
  const closePanel = () => {
    if (!panel || !panel.classList.contains('open')) return;
    panel.classList.remove('open');
    if (backdrop) backdrop.hidden = true;
    if (opener && document.contains(opener)) opener.focus();
  };
  document.addEventListener('htmx:afterSwap', (e) => {
    if (!panel || e.detail.target !== panel) return;
    const elt = e.detail.requestConfig && e.detail.requestConfig.elt;
    $$('.hit-word[aria-current="true"]').forEach((a) => a.removeAttribute('aria-current'));
    if (elt && elt.classList.contains('hit-word')) { elt.setAttribute('aria-current', 'true'); opener = elt; }
    panel.classList.add('open');
    if (modal() && backdrop) backdrop.hidden = false;
    const title = $('#panel-title', panel);
    if (title) { title.tabIndex = -1; title.focus({ preventScroll: !isSheet() }); }
  });
  document.addEventListener('click', (e) => { if (e.target.closest('[data-close-panel]')) closePanel(); });
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    if (panel && panel.classList.contains('open') && isSheet()) { closePanel(); return; }
    const f = $('details.filters[open]');
    if (f) { f.open = false; $('summary', f).focus(); }
  });

  // Filters: a "Done" button (the sheet also closes on its backdrop and Esc).
  $$('[data-close-filters]').forEach((b) => {
    b.hidden = false;
    b.addEventListener('click', () => { const d = b.closest('details'); d.open = false; $('summary', d).focus(); });
  });
  // On phones the level row scrolls: show the chosen level.
  const lv = $('.levels input:checked');
  if (lv) {
    const o = lv.closest('label');
    let sc = o.parentElement;
    while (sc && sc.scrollWidth <= sc.clientWidth) sc = sc.parentElement === document.body ? null : sc.parentElement;
    if (sc) sc.scrollLeft += o.getBoundingClientRect().left - sc.getBoundingClientRect().left - 16;
  }

  // "/" focuses the search box.
  document.addEventListener('keydown', (e) => {
    const q = $('#q') || $('#dq');
    if (q && e.key === '/' && !typing(document.activeElement)) {
      e.preventDefault();
      q.focus();
      q.select();
    }
  });

  // Dictionary autocomplete: arrow keys, Enter, Escape.
  const dq = $('#dq');
  const box = $('#suggest');
  if (dq && box) {
    let active = -1;
    const items = () => $$('a.sg', box);
    const open = (on) => { box.hidden = !on; dq.setAttribute('aria-expanded', String(on)); };
    const select = (i) => {
      const list = items();
      active = list.length ? (i + list.length) % list.length : -1;
      list.forEach((a, j) => a.setAttribute('aria-selected', String(j === active)));
      if (active >= 0) { list[active].scrollIntoView({ block: 'nearest' }); dq.setAttribute('aria-activedescendant', list[active].id = 'sg-' + active); }
    };
    document.addEventListener('htmx:afterSwap', (e) => {
      if (e.detail.target !== box) return;
      active = -1;
      open(dq.value.trim() !== '' && box.children.length > 0);
    });
    dq.addEventListener('keydown', (e) => {
      if (e.isComposing) return; // let Vietnamese input methods finish
      if (e.key === 'ArrowDown') { e.preventDefault(); open(true); select(active + 1); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); select(active - 1); }
      else if (e.key === 'Enter' && active >= 0) { e.preventDefault(); location.href = items()[active].href; }
      else if (e.key === 'Escape') { open(false); }
    });
    document.addEventListener('click', (e) => { if (!e.target.closest('.search-form')) open(false); });
    open(false);
  }

  // ---- writing desk ----
  const desk = $('.desk');
  const lyrics = $('#lyrics');
  if (desk && lyrics) {
    const KEY = 'pw_lyrics';
    const store = {
      get: () => { try { return localStorage.getItem(KEY) || ''; } catch { return ''; } },
      set: (v) => { try { v ? localStorage.setItem(KEY, v) : localStorage.removeItem(KEY); return true; } catch { return false; } },
    };
    const status = $('#save-status');
    const changed = () => lyrics.dispatchEvent(new Event('input', { bubbles: true }));

    if (desk.dataset.desk === 'guest') {
      lyrics.value = store.get();
      let t;
      lyrics.addEventListener('input', () => {
        clearTimeout(t);
        t = setTimeout(() => { if (status) status.textContent = store.set(lyrics.value) ? 'Đã lưu trong trình duyệt.' : 'Trình duyệt không cho lưu nháp.'; }, 500);
      });
    } else {
      const draft = store.get();
      const banner = $('#draft-banner');
      if (draft && banner) {
        banner.hidden = false;
        banner.addEventListener('click', (e) => {
          const b = e.target.closest('[data-draft]');
          if (!b) return;
          if (b.dataset.draft === 'use') {
            lyrics.value = lyrics.value ? lyrics.value.replace(/\s*$/, '\n\n') + draft : draft;
            changed();
          }
          store.set('');
          banner.hidden = true;
        });
      }
    }

    // The words to rhyme: the selection, else the last two words of the line
    // being written (of the previous line when the caret starts an empty one).
    let caret = 0;
    const target = () => {
      const { value, selectionStart: s, selectionEnd: e } = lyrics;
      const sel = value.slice(s, e).trim();
      if (sel && sel.length <= 40 && !sel.includes('\n')) return sel;
      const lines = value.slice(0, s).split('\n');
      let line = lines.pop();
      const rest = value.slice(s).split('\n')[0];
      line = (line + rest).trim();
      while (!line && lines.length) line = lines.pop().trim();
      return line.replace(/[.,!?;:"“”'…()\-–—]+/g, ' ').trim().split(/\s+/).slice(-2).join(' ');
    };
    const q = $('#q');
    const rform = $('#rhyme-form');
    let qt;
    const follow = (e) => {
      if (e && e.isComposing) return;
      caret = lyrics.selectionStart;
      clearTimeout(qt);
      qt = setTimeout(() => {
        const t = target();
        if (!t || !q || t === q.value || !window.htmx) return;
        q.value = t;
        window.htmx.trigger(rform, 'pw:query');
      }, 350);
    };
    ['keyup', 'click', 'select', 'focus'].forEach((ev) => lyrics.addEventListener(ev, follow));

    // ---- rhyme check: colour the multi-syllable rhymes ----
    // While checking, a mirror div under the textarea draws the text, with a
    // bright fill under black ink per rhyme group and the group's letter on
    // each chain; the textarea keeps only its caret and selection. It stays
    // a real textarea, so Vietnamese input methods, undo and autosave are
    // untouched.
    const toggle = $('#rc-toggle');
    const mirror = $('#lyrics-mirror');
    const wrap = mirror && mirror.parentElement;
    const panel = $('#rc-panel');
    if (toggle && mirror && panel) {
      let on = false, tokens = [], groups = [], text = '', focus = 0, timer, ctrl, lastRecord = 0, recordedText = '';
      const fit = () => {
        mirror.style.top = lyrics.clientTop + 'px';
        mirror.style.left = lyrics.clientLeft + 'px';
        mirror.style.width = lyrics.clientWidth + 'px';
        mirror.style.height = lyrics.clientHeight + 'px';
        mirror.scrollTop = lyrics.scrollTop;
      };
      const cls = (g) => `rg rg${g.slot} ru${g.underline}`;
      // the textarea grows with the lyrics while checking: the whole song,
      // coloured, without scrolling inside the box
      const grow = () => {
        lyrics.style.height = 'auto';
        lyrics.style.height = lyrics.scrollHeight + lyrics.offsetHeight - lyrics.clientHeight + 'px';
      };
      const paint = () => {
        const v = lyrics.value;
        const frag = document.createDocumentFragment();
        let at = 0;
        // one mark per run of the same group: the spaces inside a chain are coloured too
        const runs = [];
        for (const t of tokens) {
          const last = runs[runs.length - 1];
          if (last && t.g && last.g === t.g && /^[ \t]*$/.test(v.slice(last.e, t.s))) last.e = t.e;
          else runs.push({ ...t });
        }
        for (const t of runs) {
          if (t.s < at || t.e > v.length) continue;
          if (t.s > at) frag.append(v.slice(at, t.s));
          const m = document.createElement('mark');
          const g = t.g && groups[t.g - 1];
          m.className = g ? cls(g) : 'rep';
          if (g) m.dataset.k = g.letter;
          if (g && focus === t.g) m.classList.add('on');
          m.textContent = v.slice(t.s, t.e);
          frag.append(m);
          at = t.e;
        }
        frag.append(v.slice(at) + '​'); // keeps a trailing empty line as tall as in the textarea
        mirror.replaceChildren(frag);
        if (focus) mirror.dataset.focus = String(focus); else delete mirror.dataset.focus;
        fit();
      };
      // Keep colours on the right words while typing: shift what follows the
      // edit, drop what the edit touched; the next analysis redoes it all.
      const shift = (old, now) => {
        let p = 0;
        while (p < old.length && p < now.length && old[p] === now[p]) p++;
        let so = old.length, sn = now.length;
        while (so > p && sn > p && old[so - 1] === now[sn - 1]) { so--; sn--; }
        const d = sn - so;
        tokens = tokens.filter((t) => t.e <= p || t.s >= so).map((t) => (t.s >= so ? { ...t, s: t.s + d, e: t.e + d } : t));
      };
      const legend = (res) => {
        const ul = $('#rc-legend');
        ul.replaceChildren(...res.groups.map((g, i) => {
          const li = document.createElement('li');
          li.className = cls(g) + (focus && focus !== i + 1 ? ' off' : '');
          const b = document.createElement('button');
          b.type = 'button';
          b.title = g.members.join(' · ');
          b.setAttribute('aria-pressed', String(focus === i + 1));
          b.setAttribute('aria-label', `Nhóm ${g.letter}, vần -${g.label}, ${g.chains} chuỗi: ${g.members.join(', ')}`);
          const strong = document.createElement('b');
          strong.textContent = g.letter;
          const rest = document.createElement('span');
          rest.textContent = `-${g.label}`;
          const n = document.createElement('span');
          n.className = 'n';
          n.textContent = `×${g.chains}`;
          b.append(strong, rest, n);
          b.addEventListener('click', () => { focus = focus === i + 1 ? 0 : i + 1; legend(res); paint(); });
          li.append(b);
          return li;
        }));
        const pctText = Math.round(res.rhymed * 100) + '%';
        $('#rc-stats').textContent = res.groups.length
          ? `${res.groups.length} nhóm vần · mật độ ${res.density.toFixed(1).replace('.', ',')} · ${pctText} âm tiết có vần`
          : 'Chưa thấy chuỗi vần đa âm nào trong cùng khổ.';
        $('#rc-scheme').textContent = res.schemes.length ? 'Sơ đồ: ' + res.schemes.join(' / ') : '';
      };
      const params = () => {
        const f = $('#rhyme-form');
        const pick = (n) => { const x = f && f.querySelector(`[name=${n}]:checked`); return x ? x.value : ''; };
        const lang = pick('lang');
        return { dialect: pick('dialect'), level: lang === 'en' ? '' : pick('level') };
      };
      const analyse = async () => {
        if (!on) return;
        if (ctrl) ctrl.abort();
        ctrl = new AbortController();
        const sent = lyrics.value;
        const record = sent !== recordedText && Date.now() - lastRecord > 60000;
        try {
          const r = await fetch('/api/v1/rhymecheck', {
            method: 'POST', signal: ctrl.signal,
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf() },
            body: JSON.stringify({ text: sent, ...params(), record }),
          });
          if (!r.ok) { $('#rc-stats').textContent = r.status === 400 ? 'Lời dài quá để kiểm tra.' : 'Chưa kiểm tra được, thử lại sau.'; return; }
          const res = await r.json();
          if (record) { lastRecord = Date.now(); recordedText = sent; }
          if (!on || lyrics.value !== sent) return; // stale: the next run will catch up
          tokens = res.tokens; groups = res.groups; text = sent;
          if (focus > groups.length) focus = 0;
          legend(res);
          paint();
        } catch (err) {
          if (err.name !== 'AbortError') $('#rc-stats').textContent = 'Chưa kiểm tra được, thử lại sau.';
        }
      };
      const setOn = async (v) => {
        on = v;
        toggle.setAttribute('aria-pressed', String(on));
        wrap.classList.toggle('checking', on);
        panel.hidden = !on;
        if (!on) { tokens = []; groups = []; mirror.replaceChildren(); lyrics.style.height = ''; return; }
        if (document.fonts && document.fonts.ready) await document.fonts.ready; // metrics must match before painting
        text = lyrics.value;
        grow();
        paint();
        analyse();
      };
      toggle.addEventListener('click', (e) => { e.preventDefault(); setOn(!on); });
      lyrics.addEventListener('input', () => {
        if (!on) return;
        shift(text, lyrics.value);
        text = lyrics.value;
        grow();
        paint();
        clearTimeout(timer);
        timer = setTimeout(analyse, 500);
      });
      lyrics.addEventListener('scroll', () => { mirror.scrollTop = lyrics.scrollTop; });
      if (window.ResizeObserver) new ResizeObserver(() => on && fit()).observe(lyrics);
      const rf = $('#rhyme-form');
      if (rf) rf.addEventListener('change', () => { clearTimeout(timer); timer = setTimeout(analyse, 150); });
    }

    // "Chèn vào bài": put the word at the caret, with spaces as needed.
    document.addEventListener('click', (e) => {
      const b = e.target.closest('[data-insert]');
      if (!b) return;
      const word = b.dataset.insert;
      const v = lyrics.value;
      const before = v.slice(0, caret), after = v.slice(caret);
      const pre = before && !/\s$/.test(before) ? ' ' : '';
      const post = after && !/^\s/.test(after) ? ' ' : '';
      lyrics.value = before + pre + word + post + after;
      caret = (before + pre + word).length;
      changed();
      closePanel();
      lyrics.focus();
      lyrics.setSelectionRange(caret, caret);
      toast(`Đã chèn “${word}”`);
    });
  }

  // ---- rhyme books ----
  // Delete with undo: the row hides at once and goes after 5 seconds (or
  // when the page is left).
  const pending = new Map();
  const flush = (f) => {
    const p = pending.get(f);
    if (!p) return;
    clearTimeout(p.timer);
    pending.delete(f);
    fetch(f.action, { method: 'POST', keepalive: true, headers: { 'X-CSRF-Token': csrf(), 'X-Requested-With': 'fetch', 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'csrf_token=' + encodeURIComponent(csrf()) })
      .then((r) => { if (r.ok) p.li.remove(); else { p.li.hidden = false; toast('Không xoá được, thử lại.'); } })
      .catch(() => { p.li.hidden = false; });
  };
  document.addEventListener('submit', (e) => {
    const f = e.target.closest('form.del');
    if (!f) return;
    e.preventDefault();
    const li = f.closest('li');
    li.hidden = true;
    pending.set(f, { li, timer: setTimeout(() => flush(f), 5000) });
    toast(`Đã xoá “${f.dataset.undo}”`, 'Hoàn tác', () => {
      const p = pending.get(f);
      if (!p) return;
      clearTimeout(p.timer);
      pending.delete(f);
      li.hidden = false;
      $('button', f).focus();
    });
  });
  window.addEventListener('pagehide', () => [...pending.keys()].forEach(flush));

  // Ordering: drag the grip (SortableJS), or focus it and press ↑ / ↓.
  const list = $('ol.sortable');
  if (list && window.htmx) {
    const save = () => {
      const ids = $$('li[data-id]', list).map((li) => li.dataset.id).join(',');
      window.htmx.ajax('PUT', list.dataset.orderUrl, { source: list, values: { ids }, swap: 'none' });
    };
    if (window.Sortable) window.Sortable.create(list, { handle: '.grip', animation: 150, onEnd: save });
    list.addEventListener('keydown', (e) => {
      const grip = e.target.closest('.grip');
      if (!grip || (e.key !== 'ArrowUp' && e.key !== 'ArrowDown')) return;
      e.preventDefault();
      const li = grip.closest('li');
      const sib = e.key === 'ArrowUp' ? li.previousElementSibling : li.nextElementSibling;
      if (!sib) return;
      e.key === 'ArrowUp' ? sib.before(li) : sib.after(li);
      grip.focus();
      const pos = $$('li[data-id]', list).indexOf(li) + 1;
      const st = $('#order-status');
      if (st) st.textContent = `Đã chuyển tới vị trí ${pos}`;
      save();
    });
  }

  // ---- slang review: j/k choose, a approve, r reject ----
  const review = $('[data-review]');
  if (review) {
    const rows = () => $$('li.item', review);
    document.addEventListener('keydown', (e) => {
      if (typing(document.activeElement) || e.metaKey || e.ctrlKey || e.altKey) return;
      const all = rows();
      let i = all.indexOf(document.activeElement.closest && document.activeElement.closest('li.item'));
      if (e.key === 'j' || e.key === 'k') {
        e.preventDefault();
        i = Math.min(Math.max(i + (e.key === 'j' ? 1 : -1), 0), all.length - 1);
        if (all[i]) all[i].focus();
      } else if ((e.key === 'a' || e.key === 'r') && i >= 0) {
        const b = $(`button[data-key="${e.key}"]`, all[i]);
        if (b) { e.preventDefault(); b.click(); }
      }
    });
    // keep the cursor on the next row after a decision
    let next = null;
    document.addEventListener('htmx:beforeSwap', (e) => {
      const li = e.detail.target;
      next = review.contains(li) ? li.nextElementSibling : null;
    });
    document.addEventListener('htmx:afterSettle', () => { if (next) { next.focus(); next = null; } });
  }
})();
