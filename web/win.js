"use strict";
// Own window frame: drag by the title area, window buttons, resize edges (Windows build binds window.opcWin).
(function () {
  const has = typeof window.opcWin === "function";
  if (!has && /Mac/.test(navigator.platform)) document.documentElement.classList.add("native-frame");
  const W = (op, a) => has ? window.opcWin(op, a || 0) : Promise.resolve(false);
  const set = z => document.documentElement.classList.toggle("maxed", !!z);
  const icons = {
    min: '<svg viewBox="0 0 12 12"><path d="M2 6.5h8"/></svg>',
    max: '<svg viewBox="0 0 12 12"><rect x="2" y="2" width="8" height="8" rx="1"/></svg>',
    close: '<svg viewBox="0 0 12 12"><path d="m2.5 2.5 7 7m0-7-7 7"/></svg>',
  };
  document.querySelectorAll("[data-win]").forEach(b => {
    b.innerHTML = icons[b.dataset.win];
    b.onclick = () => W(b.dataset.win).then(set);
  });
  const edges = {n: 12, s: 15, w: 10, e: 11, nw: 13, ne: 14, sw: 16, se: 17};
  if (document.body.dataset.resizable) for (const k in edges) {
    const d = document.createElement("div"); d.className = "edge e-" + k;
    d.onmousedown = e => { if (e.button === 0) { e.preventDefault(); W("resize", edges[k]); } };
    document.body.appendChild(d);
  }
  const frame = document.createElement("div"); frame.className = "winframe"; document.body.appendChild(frame);
  // start the native move only once the mouse actually moves, so clicks and double-clicks stay normal
  let down = null;
  const isDrag = t => t.closest("[data-drag]") && !t.closest("button,input,select,textarea,a,canvas,[data-nodrag]");
  document.addEventListener("mousedown", e => { down = e.button === 0 && isDrag(e.target) ? [e.screenX, e.screenY] : null; });
  document.addEventListener("mousemove", e => {
    if (!down || !(e.buttons & 1)) { down = null; return; }
    if (Math.abs(e.screenX - down[0]) + Math.abs(e.screenY - down[1]) > 3) { down = null; W("drag"); }
  });
  document.addEventListener("mouseup", () => down = null);
  document.addEventListener("dblclick", e => { if (isDrag(e.target) && document.body.dataset.resizable) W("max").then(set); });
  window.addEventListener("resize", () => W("state").then(set));
  W("state").then(set);
})();
