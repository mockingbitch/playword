// Small progressive enhancements; the site works without them.
(() => {
  // Tapping an open result closes it instead of fetching it again.
  document.addEventListener('htmx:beforeRequest', (e) => {
    const btn = e.detail.elt;
    if (!btn.classList || !btn.classList.contains('hit-head')) return;
    const def = btn.parentElement.querySelector('.def');
    if (def && def.childElementCount) {
      e.preventDefault();
      def.replaceChildren();
      btn.setAttribute('aria-expanded', 'false');
    } else {
      btn.setAttribute('aria-expanded', 'true');
    }
  });

  // "/" focuses the search box.
  document.addEventListener('keydown', (e) => {
    const q = document.getElementById('q') || document.getElementById('dq');
    if (q && e.key === '/' && document.activeElement !== q) {
      e.preventDefault();
      q.focus();
      q.select();
    }
  });

  // Dictionary autocomplete: arrow keys, Enter, Escape.
  const dq = document.getElementById('dq');
  const box = document.getElementById('suggest');
  if (!dq || !box) return;
  let active = -1;
  const items = () => [...box.querySelectorAll('a.sg')];
  const open = (on) => { box.hidden = !on; dq.setAttribute('aria-expanded', String(on)); };
  const select = (i) => {
    const list = items();
    active = list.length ? (i + list.length) % list.length : -1;
    list.forEach((a, j) => a.setAttribute('aria-selected', String(j === active)));
    if (active >= 0) list[active].scrollIntoView({ block: 'nearest' });
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
})();

// Drag-and-drop ordering of a rhyme book (SortableJS, vendored for the CSP).
document.addEventListener('DOMContentLoaded', () => {
  const list = document.querySelector('ol.sortable');
  if (!list || !window.Sortable || !window.htmx) return;
  window.Sortable.create(list, {
    handle: '.grip',
    animation: 150,
    onEnd: () => {
      const ids = [...list.querySelectorAll('li[data-id]')].map((li) => li.dataset.id).join(',');
      window.htmx.ajax('PUT', list.dataset.orderUrl, { source: list, values: { ids }, swap: 'none' });
    },
  });
});
