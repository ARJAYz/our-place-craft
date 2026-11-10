"use strict";
const TOKEN = location.hash.slice(1);
const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));

async function api(method, ...args) {
  const r = await fetch("/api/" + method, {method: "POST", headers: {"X-Token": TOKEN}, body: JSON.stringify(args)});
  const j = await r.json();
  if (j.error) throw new Error(j.error);
  return j.result;
}
// long calls return a job id: wait for it
async function job(method, ...args) {
  const id = await api(method, ...args);
  for (;;) {
    await new Promise(r => setTimeout(r, 150));
    const j = await api("Job", id);
    if (j.done) { if (j.error) throw new Error(j.error); return j.result; }
  }
}

let S = null;           // last state
let page = "home";

function selected() {
  if (!S || !S.instances?.length) return null;
  return S.instances.find(i => i.id === S.settings.selected) || S.instances[0];
}
const isAdv = i => i && i.kind === "adventure";            // installed by the old launcher
const isOPC = i => i && (i.kind === "adventure" || i.kind === "pack"); // our Adventure pack
const artOf = i => isOPC(i) ? "a/art_modded.png" : (i.loader === "vanilla" ? "a/art_vanilla.png" : "a/art_modded.png");
const iconOf = i => i.loader === "vanilla" ? "a/grass.png" : (isOPC(i) ? "a/grass.png" : "a/stone.png");
const subOf = i => `Minecraft ${i.mc} · ${i.loader === "vanilla" ? "без модов" : i.loader_name + (i.loader_version ? " " + shortLV(i) : "")}`;
const shortLV = i => i.loader === "forge" ? i.loader_version.replace(i.mc + "-", "") : i.loader_version;

function show(p) {
  page = p;
  $$(".tab").forEach(t => t.classList.toggle("on", t.dataset.page === p));
  $$(".page").forEach(s => s.classList.toggle("on", s.id === "page-" + p));
  if (p === "tweaks") renderTweaks();
  if (p === "settings") { $("#nick").value = S?.settings.nick || ""; $("#skin").value = S?.settings.skin || "";
    $("#cfkey").value = S?.settings.cf_key || ""; }
}
$$("[data-page]").forEach(b => b.addEventListener("click", () => show(b.dataset.page)));

// ---------------------------------------------------------------- rendering
function render() {
  const sel = selected();
  $("#nickTop").textContent = S.settings.nick || "Игрок";
  drawHead();
  renderServers();
  // mini list
  $("#miniList").innerHTML = S.instances.map(i => `<button class="mi ${sel && i.id === sel.id ? "on" : ""}" data-id="${i.id}">
      <img src="${iconOf(i)}"><div><b>${esc(i.name)}</b><small>${esc(subOf(i))}</small></div><span class="gear" data-edit="${i.id}" title="Настройки и загрузчик">⚙</span><span class="pl" data-play="${i.id}" title="Играть">▶</span></button>`).join("") ||
    `<div class="muted">Пока пусто.</div>`;
  $$("#miniList .mi").forEach(b => b.onclick = e => {
    const id = b.dataset.id;
    if (e.target.dataset.edit) { openEdit(e.target.dataset.edit); return; }
    if (e.target.dataset.play) { const i = S.instances.find(x => x.id === id); select(id).then(() => play(id, !!(i.server || isOPC(i)))); }
    else select(id);
  });
  // grid
  $("#grid").innerHTML = S.instances.map(i => `<div class="card ${sel && i.id === sel.id ? "sel" : ""}" data-id="${i.id}">
      <div class="art" style="background-image:url(${artOf(i)})"></div>
      <div class="cbody"><h4>${esc(i.name)}</h4><div class="sub">${esc(subOf(i))}</div>
        <div class="muted">${i.server ? "Сервер: " + esc(i.server) : "Без сервера"} · ${Math.round(i.ram_mb / 1024)} ГБ</div>
        <div class="acts"><button class="go" data-act="play">▶ Играть</button><button data-act="folder">Папка</button><button data-act="edit">Настройки и загрузчик</button></div>
      </div></div>`).join("") + `<div class="card empty" onclick="openCreate()"><b style="font-size:40px;color:#3f86d9">+</b><br>Создать сборку</div>`;
  $$("#grid .card[data-id]").forEach(c => {
    c.onclick = e => {
      const id = c.dataset.id, act = e.target.dataset?.act;
      if (act === "play") { e.stopPropagation(); select(id).then(() => play(id, !!(S.instances.find(x => x.id === id)?.server || isOPC(S.instances.find(x => x.id === id))))); }
      else if (act === "folder") { e.stopPropagation(); api("OpenFolder", id); }
      else if (act === "edit") { e.stopPropagation(); openEdit(id); }
      else { select(id); show("home"); }
    };
  });
  renderMiniTweaks();
  // footer
  $("#status").textContent = S.stage || S.status;
  $("#status").classList.toggle("err", S.is_err && !S.busy);
  $("#fdot").className = "dot" + (S.busy && !S.running ? " busy" : (S.is_err ? " err" : ""));
  const pr = $("#progress");
  pr.classList.toggle("on", !!S.busy && !S.running);
  pr.classList.toggle("ind", !(S.total > 0));
  $("#pbar").style.width = S.total > 0 ? Math.min(100, S.done * 100 / S.total) + "%" : "";
  $("#footInfo").textContent = sel ? `${sel.name} · Minecraft ${sel.mc}` : "";
}

// ---------------------------------------------------------------- my servers
let SV = [];
let PI = {};
async function loadServers() {
  try { SV = await api("Servers"); } catch (e) { SV = []; }
  try { PI = await api("PackInfo"); } catch (e) { PI = {}; }
  renderServers(); renderSrvList();
}
const fmtVer = v => v && v.length === 13 ? `${v.slice(6,8)}.${v.slice(4,6)}.${v.slice(0,4)} ${v.slice(9,11)}:${v.slice(11,13)}` : (v || "");
function badge(st) {
  if (!st) return `<div class="badge wait"><span class="dot busy"></span>Проверяем…</div>`;
  if (!st.online) return `<div class="badge off"><span class="dot err"></span>Не в сети</div>`;
  return `<div class="badge"><span class="dot"></span><b style="color:var(--green2)">Онлайн</b>&nbsp;${st.players} / ${st.max}</div>`;
}
// a vanilla server played with a mod loader on the client (set in the build's settings)
function clientLoader(v) {
  const i = S.instances.find(x => x.id === v.instance);
  return i && !isAdv(i) && i.kind !== "pack" && i.loader !== "vanilla" && !/NeoForge|Fabric|Forge|Quilt/.test(v.subtitle) ? ` · у вас ${esc(i.loader_name)}` : "";
}
function renderLegacy() {
  const old = S && S.instances.find(i => i.kind === "adventure");
  let bar = $("#legacyBar");
  if (!old) { if (bar) bar.remove(); return; }
  if (bar) return;
  $("#serverCards").insertAdjacentHTML("beforebegin", `<div class="legacybar" id="legacyBar">
    <span>📦 Сборка Adventure запускается из <b>старого лаунчера</b> (${esc(old.root || "")}). Перенесите её сюда — миры и настройки сохранятся, старый лаунчер удалится.</span>
    <button class="play small" id="migBtn">Перенести</button></div>`);
  $("#migBtn").onclick = async () => {
    if (!confirm("Перенести сборку, миры и настройки в новый лаунчер и удалить старый лаунчер?\n\nСначала закройте старый лаунчер и игру.")) return;
    $("#migBtn").disabled = true;
    try { await job("MigrateLegacy"); alertOk("Готово: всё перенесено, старый лаунчер удалён"); await refresh(); loadServers(); }
    catch (e) { alertBar(e.message); $("#migBtn").disabled = false; }
  };
}
function renderServers() {
  const box = $("#serverCards");
  if (!box || !S) return;
  renderLegacy();
  if (S.locked) {
    if (!box.querySelector(".lockcard")) {
      box.innerHTML = `<div class="card lockcard">
        <div class="lockic">🔒</div>
        <h2>Серверы Our Place Craft</h2>
        <p class="muted">Серверы и сборка Adventure открываются по коду доступа. Код даёт владелец сервера.</p>
        <div class="lockrow"><input id="unlockIn" type="password" autocomplete="off" placeholder="Код доступа"><button class="play small" id="unlockBtn">Открыть</button></div>
        <p class="err" id="unlockErr"></p>
        <p class="muted small">Без кода лаунчер тоже работает: создавайте свои сборки во вкладке «Сборки».</p>
      </div>`;
      const go = async () => {
        $("#unlockErr").textContent = "";
        try { await api("Unlock", $("#unlockIn").value); await refresh(); await loadServers(); alertOk("Серверы открыты!"); }
        catch (e) { $("#unlockErr").textContent = e.message; }
      };
      $("#unlockBtn").onclick = go;
      $("#unlockIn").onkeydown = e => { if (e.key === "Enter") go(); };
    }
    return;
  }
  const vis = SV.filter(v => !v.hidden), hid = SV.filter(v => v.hidden);
  const busy = !!S.busy;
  box.innerHTML = vis.map(v => `<div class="card big srv" data-id="${v.id}">
      <div class="art" style="background-image:url(${v.art})">${badge(v.status)}
        <button class="more" data-more="${v.id}" title="Ещё">⋯</button>
        <div class="menu" id="menu-${v.id}">${S.instances.some(x => x.id === v.instance) ? `<button data-repair="${v.instance}">🔧 Починить сборку…</button>` : ""}${v.instance !== "our-place-craft" && v.id !== "adventure" && S.instances.some(x => x.id === v.instance) ? `<button data-edit="${v.instance}">Загрузчик и настройки…</button>` : ""}<button data-hide="${v.id}">Скрыть с главной…</button><button data-folder="${v.instance}">Открыть папку</button></div></div>
      <div class="cbody"><h2>${esc(v.name)}</h2><div class="sub">${esc(v.subtitle)}${clientLoader(v)}</div>
        <p class="muted desc">${esc(v.desc)}</p>
        ${v.installed ? `<button class="play main" style="width:100%" data-play="${v.instance}" ${busy ? "disabled" : ""}>ИГРАТЬ</button>`
          : `<button class="play" style="width:100%" data-install ${busy ? "disabled" : ""}>⬇ УСТАНОВИТЬ СБОРКУ</button>`}
        ${v.id === "adventure" && v.installed && PI.latest && PI.version && PI.latest !== PI.version && !PI.legacy
          ? `<div class="upd">Есть обновление сборки от ${esc(fmtVer(PI.latest))} — скачается при запуске</div>` : ""}
        <div class="row field ip"><span>IP: ${esc(v.address)}</span><button data-copy="${esc(v.address)}">⧉ скопировать</button></div>
      </div></div>`).join("") +
    (hid.length ? `<div class="hidden-note">Скрыто серверов: ${hid.length}. <button data-page-go="settings">Показать в настройках</button></div>` : "") +
    (!vis.length && !hid.length ? `<div class="card"><div class="cbody" style="margin-top:0;padding-top:16px"><h2>Серверов нет</h2><p class="muted desc">Создайте сборку во вкладке «Сборки».</p></div></div>` : "");
  $$("[data-play]", box).forEach(b => b.onclick = () => select(b.dataset.play).then(() => play(b.dataset.play, true)));
  $$("[data-install]", box).forEach(b => b.onclick = () => installPack());
  $$("[data-more]", box).forEach(b => b.onclick = e => { e.stopPropagation(); const m = $("#menu-" + b.dataset.more); const on = !m.classList.contains("on"); closeMenus(); m.classList.toggle("on", on); });
  $$("[data-repair]", box).forEach(b => b.onclick = () => { closeMenus(); openRepair(b.dataset.repair); });
  $$("[data-edit]", box).forEach(b => b.onclick = () => { closeMenus(); openEdit(b.dataset.edit); });
  $$("[data-hide]", box).forEach(b => b.onclick = () => { closeMenus(); askHide(b.dataset.hide); });
  $$("[data-folder]", box).forEach(b => b.onclick = () => { closeMenus(); api("OpenFolder", b.dataset.folder); });
  $$("[data-copy]", box).forEach(b => b.onclick = () => { navigator.clipboard?.writeText(b.dataset.copy); b.textContent = "✓ скопировано"; setTimeout(() => b.textContent = "⧉ скопировать", 1500); });
  $$("[data-page-go]", box).forEach(b => b.onclick = () => show(b.dataset.pageGo));
}
async function installPack() {
  let code = "";
  if (!PI.has_code) {
    $("#codeDlg").classList.add("on"); $("#codeIn").value = ""; $("#codeErr").textContent = ""; $("#codeIn").focus();
    return;
  }
  runInstallPack(code);
}
async function runInstallPack(code) {
  try { refresh(); await job("InstallPack", code); alertOk("Сборка установлена — нажмите «Играть»"); }
  catch (e) { alertBar(e.message); }
  await refresh(); loadServers();
}
$("#codeOk").onclick = () => {
  const c = $("#codeIn").value.trim();
  if (!c) { $("#codeErr").textContent = "Введите код"; return; }
  $("#codeDlg").classList.remove("on"); runInstallPack(c);
};
function closeMenus() { $$(".menu.on").forEach(m => m.classList.remove("on")); }
document.addEventListener("click", closeMenus);
function renderSrvList() {
  const box = $("#srvList");
  if (!box) return;
  box.innerHTML = SV.map(v => `<button class="t" data-sid="${v.id}"><span>${esc(v.name)} <small class="muted">${esc(v.address)}</small></span><span class="sw ${v.hidden ? "" : "on"}"></span></button>`).join("")
    || `<div class="muted">Серверов нет.</div>`;
  $$("#srvList .t").forEach(b => b.onclick = async () => {
    const v = SV.find(x => x.id === b.dataset.sid);
    if (!v.hidden) { askHide(v.id); return; }      // hiding always asks
    await api("HideServer", v.id, false); loadServers();
  });
}
// hiding needs the button held for 1.6 s, so it cannot happen by a stray click
let hideID = "", holdTimer = 0;
function askHide(id) {
  const v = SV.find(x => x.id === id); if (!v) return;
  hideID = id;
  $("#hideText").innerHTML = `Сервер <b>${esc(v.name)}</b> (${esc(v.address)}) пропадёт с главной страницы.`;
  $("#hideDlg").classList.add("on");
}
const hold = $("#holdBtn");
const holdStart = e => { e.preventDefault(); hold.classList.add("go"); holdTimer = setTimeout(async () => {
  hold.classList.remove("go"); $("#hideDlg").classList.remove("on");
  await api("HideServer", hideID, true); loadServers(); alertOk("Сервер скрыт. Вернуть — «Настройки» → «Серверы на главной».");
}, 1600); };
const holdStop = () => { clearTimeout(holdTimer); hold.classList.remove("go"); };
hold.addEventListener("mousedown", holdStart); hold.addEventListener("touchstart", holdStart);
["mouseup", "mouseleave", "touchend", "touchcancel"].forEach(ev => hold.addEventListener(ev, holdStop));

let tweaksCache = {key: "", list: []};
async function loadTweaks(force) {
  const sel = selected();
  if (!sel) return [];
  const key = sel.id;
  if (force || tweaksCache.key !== key) tweaksCache = {key, list: await api("Tweaks", sel.id)};
  return tweaksCache.list;
}
async function renderMiniTweaks() {
  const list = (await loadTweaks()).slice(0, 6);
  $("#miniTweaks").innerHTML = list.map(t => `<button class="t ${t.note ? "off" : ""}" data-t="${t.id}" title="${esc(t.note || t.desc)}"><span>${esc(t.title.split(" — ")[0])}</span><span class="sw ${t.on ? "on" : ""}"></span></button>`).join("")
    || `<div class="muted">Нет сборки.</div>`;
  $$("#miniTweaks .t").forEach(b => b.onclick = () => toggleTweak(b.dataset.t));
}
async function renderTweaks() {
  const sel = selected();
  $("#tweaksFor").textContent = sel ? `${sel.name} · ${subOf(sel)}` : "";
  const list = await loadTweaks(true);
  $("#tweakList").innerHTML = list.map(t => `<button class="trow ${t.note ? "off" : ""}" data-t="${t.id}">
      <div><b>${esc(t.title)}</b><p>${esc(t.desc)}</p>${t.note ? `<em>${esc(t.note)}</em>` : ""}</div><span class="sw ${t.on ? "on" : ""}"></span></button>`).join("");
  $$("#tweakList .trow").forEach(b => b.onclick = () => toggleTweak(b.dataset.t));
}
async function toggleTweak(id) {
  const sel = selected();
  const t = tweaksCache.list.find(x => x.id === id);
  if (!sel || !t || t.note) return;
  await api("SetTweak", sel.id, id, !t.on);
  await loadTweaks(true);
  renderMiniTweaks();
  if (page === "tweaks") renderTweaks();
}

async function select(id) { await api("Select", id); S.settings.selected = id; tweaksCache.key = ""; render(); }
async function play(id, toServer) {
  try { await api("Play", id, toServer); } catch (e) { alertBar(e.message); }
  refresh();
}
function alertBar(msg) { $("#status").textContent = msg; $("#status").classList.add("err"); $("#fdot").className = "dot err"; }

// player head: Steve-like placeholder (the game shows the real skin)
function drawHead() {
  const c = $("#head").getContext("2d");
  const hair = "#3b2a1a", skin = "#c9905f", eye = "#3b4fa8";
  c.fillStyle = skin; c.fillRect(0, 0, 8, 8);
  c.fillStyle = hair; c.fillRect(0, 0, 8, 2); c.fillRect(0, 2, 1, 1); c.fillRect(7, 2, 1, 1);
  c.fillStyle = "#fff"; c.fillRect(1, 4, 1, 1); c.fillRect(6, 4, 1, 1);
  c.fillStyle = eye; c.fillRect(2, 4, 1, 1); c.fillRect(5, 4, 1, 1);
  c.fillStyle = "#8a4e3a"; c.fillRect(3, 6, 2, 1);
}

// ---------------------------------------------------------------- create dialog
let C = {mc: "", loader: "vanilla", lv: "", versions: []};
function openCreate() {
  C = {mc: "", loader: "vanilla", lv: "", versions: []};
  $("#cName").value = ""; $("#cServer").value = ""; $("#cErr").textContent = ""; $("#cFilter").value = "";
  $("#cRam").max = Math.max(4, Math.floor((S?.max_ram_mb || 16384) / 1024));
  $("#cRam").value = 6; $("#cRamV").textContent = "6 ГБ";
  $$("#cLoaders button").forEach(b => b.classList.toggle("on", b.dataset.l === "vanilla"));
  $("#cLoaderVersions").innerHTML = `<div class="muted pad">Чистый Minecraft без модов.</div>`;
  $("#createDlg").classList.add("on");
  loadVersions();
}
window.openCreate = openCreate;
$("#newBtn").onclick = openCreate; $("#newBtn2").onclick = openCreate;
$$("[data-close]").forEach(b => b.onclick = () => b.closest(".modal").classList.remove("on"));
$("#cRam").oninput = () => $("#cRamV").textContent = $("#cRam").value + " ГБ";
$("#cSnap").onchange = loadVersions;
$("#cFilter").oninput = renderVersions;

async function loadVersions() {
  $("#cVersions").innerHTML = `<div class="muted pad">Загружаем список версий…</div>`;
  try { C.versions = await job("Versions", $("#cSnap").checked); renderVersions(); }
  catch (e) { $("#cVersions").innerHTML = `<div class="err pad">${esc(e.message)}</div>`; }
}
function renderVersions() {
  const f = $("#cFilter").value.trim();
  const list = C.versions.filter(v => !f || v.id.includes(f));
  if (!C.mc && list.length) C.mc = list[0].id;
  $("#cVersions").innerHTML = list.slice(0, 400).map(v => `<button data-v="${esc(v.id)}" class="${v.id === C.mc ? "on" : ""}">
    <span>${esc(v.id)} ${v.type !== "release" ? `<small class="beta">${v.type === "snapshot" ? "снапшот" : "старая"}</small>` : ""}</span><small>${v.date}</small></button>`).join("")
    || `<div class="muted pad">Ничего не найдено.</div>`;
  $$("#cVersions button").forEach(b => b.onclick = () => { C.mc = b.dataset.v; renderVersions(); loadLoaderVersions(); });
  loadLoaderVersions();
}
$$("#cLoaders button").forEach(b => b.onclick = () => {
  C.loader = b.dataset.l; C.lv = "";
  $$("#cLoaders button").forEach(x => x.classList.toggle("on", x === b));
  loadLoaderVersions();
});
let lvSeq = 0;
async function loadLoaderVersions() {
  const box = $("#cLoaderVersions");
  if (C.loader === "vanilla") { C.lv = ""; box.innerHTML = `<div class="muted pad">Чистый Minecraft без модов. Подходит для ванильного сервера.</div>`; return; }
  if (!C.mc) return;
  const seq = ++lvSeq;
  box.innerHTML = `<div class="muted pad">Загружаем версии…</div>`;
  try {
    const list = await job("LoaderVersions", C.loader, C.mc);
    if (seq !== lvSeq) return;
    C.lv = (list.find(v => v.stable) || list[0]).version;
    const draw = () => {
      box.innerHTML = list.map((v, i) => `<button data-v="${esc(v.version)}" class="${v.version === C.lv ? "on" : ""}">
        <span>${esc(v.version)}</span><small class="${v.stable ? "tag" : "beta"}">${v.version === (list.find(x => x.stable) || list[0]).version ? "рекомендуемая" : (v.stable ? "" : "бета")}</small></button>`).join("");
      $$("#cLoaderVersions button").forEach(b => b.onclick = () => { C.lv = b.dataset.v; draw(); });
    };
    draw();
  } catch (e) { if (seq === lvSeq) { C.lv = ""; box.innerHTML = `<div class="err pad">${esc(e.message)}</div>`; } }
}
$("#cOk").onclick = async () => {
  $("#cErr").textContent = "";
  try {
    const id = await api("Create", {name: $("#cName").value, mc: C.mc, loader: C.loader, loader_version: C.lv,
      server: $("#cServer").value, ram_mb: +$("#cRam").value * 1024});
    $("#createDlg").classList.remove("on");
    await refresh(); await select(id); show("home");
  } catch (e) { $("#cErr").textContent = e.message; }
};

// ---------------------------------------------------------------- edit dialog
let E = null;
function openEdit(id) {
  E = S.instances.find(i => i.id === id); if (!E) return;
  $("#eName").value = E.name; $("#eServer").value = E.server || "";
  $("#eRam").max = Math.max(4, Math.floor((S.max_ram_mb || 16384) / 1024));
  $("#eRam").value = Math.round(E.ram_mb / 1024); $("#eRamV").textContent = $("#eRam").value + " ГБ";
  $("#eInfo").textContent = `${subOf(E)}. Папка: ${E.folder}`;
  $("#eDel").textContent = isAdv(E) ? "Убрать из списка" : "Удалить сборку";
  $("#eErr").textContent = "";
  EL = {loader: E.loader || "vanilla", lv: E.loader_version || ""};
  $("#eLoaderBox").hidden = isAdv(E) || E.kind === "pack";
  drawEditLoaders(); loadEditLoaderVersions();
  $("#editDlg").classList.add("on");
}
let EL = {loader: "vanilla", lv: ""}, elSeq = 0;
function drawEditLoaders() {
  $$("#eLoaders button").forEach(b => b.classList.toggle("on", b.dataset.l === EL.loader));
  const changed = EL.loader !== (E.loader || "vanilla");
  $("#eLoaderNote").textContent = !changed ? "" : EL.loader === "vanilla"
    ? "Моды отложатся в сторону и вернутся, если снова выбрать этот загрузчик. Миры не трогаются."
    : `При следующем «Играть» скачается ${$(`#eLoaders [data-l=${EL.loader}]`).textContent}. Моды прежнего загрузчика отложатся и вернутся, если переключиться обратно. Миры не трогаются.`;
}
$$("#eLoaders button").forEach(b => b.onclick = () => {
  EL.loader = b.dataset.l; EL.lv = b.dataset.l === (E.loader || "vanilla") ? (E.loader_version || "") : "";
  drawEditLoaders(); loadEditLoaderVersions();
});
async function loadEditLoaderVersions() {
  const box = $("#eLoaderVersions");
  if (EL.loader === "vanilla") { EL.lv = ""; box.innerHTML = `<div class="muted pad">Чистый Minecraft ${esc(E.mc)} без модов.</div>`; return; }
  const seq = ++elSeq;
  box.innerHTML = `<div class="muted pad">Загружаем версии для Minecraft ${esc(E.mc)}…</div>`;
  try {
    const list = await job("LoaderVersions", EL.loader, E.mc);
    if (seq !== elSeq) return;
    const rec = (list.find(v => v.stable) || list[0]).version;
    if (!EL.lv || !list.some(v => v.version === EL.lv)) EL.lv = rec;
    const draw = () => {
      box.innerHTML = list.map(v => `<button data-v="${esc(v.version)}" class="${v.version === EL.lv ? "on" : ""}">
        <span>${esc(v.version)}</span><small class="${v.stable ? "tag" : "beta"}">${v.version === rec ? "рекомендуемая" : (v.stable ? "" : "бета")}</small></button>`).join("");
      $$("#eLoaderVersions button").forEach(b => b.onclick = () => { EL.lv = b.dataset.v; draw(); });
    };
    draw();
  } catch (e) { if (seq === elSeq) { EL.lv = ""; box.innerHTML = `<div class="err pad">${esc(e.message)}</div>`; } }
}
window.openEdit = openEdit;
$("#eRepair").onclick = () => { $("#editDlg").classList.remove("on"); openRepair(E.id); };

// ---------------------------------------------------------------- repair
async function openRepair(id) {
  const i = S.instances.find(x => x.id === id); if (!i) return;
  $("#repTitle").textContent = "Починить сборку";
  $("#repBody").innerHTML = `<p><b>${esc(i.name)}</b> · ${esc(subOf(i))}</p>
    <p class="muted">Лаунчер проверит каждый файл игры${i.kind === "pack" || i.kind === "adventure" ? " и сборки" : ""} и скачает заново испорченные и пропавшие. Миры, скриншоты, настройки управления и твики не трогаются.</p>`;
  $("#repFoot").innerHTML = `<span class="err" id="repErr"></span><button class="ghost" data-close>Отмена</button><button class="play small" id="repGo">Проверить и починить</button>`;
  $$("#repFoot [data-close]").forEach(b => b.onclick = () => $("#repDlg").classList.remove("on"));
  $("#repDlg").classList.add("on");
  $("#repGo").onclick = () => runRepair(i);
}
async function runRepair(i) {
  $("#repBody").innerHTML = `<p><b>${esc(i.name)}</b></p><p id="repStage" class="muted">Начинаем проверку…</p>
    <div class="progress on" style="height:10px;border-radius:5px"><i id="repBar"></i></div>`;
  $("#repFoot").innerHTML = `<span class="muted">Можно закрыть окно — проверка продолжится</span><button class="ghost" id="repHide">Скрыть</button>`;
  $("#repHide").onclick = () => $("#repDlg").classList.remove("on");
  const tick = setInterval(() => {
    if (!S) return;
    $("#repStage") && ($("#repStage").textContent = S.stage || S.status);
    $("#repBar") && ($("#repBar").style.width = S.total > 0 ? Math.min(100, S.done * 100 / S.total) + "%" : "15%");
  }, 300);
  try {
    const r = await job("Repair", i.id);
    clearInterval(tick);
    const n = (a, one, few, many) => { const k = a % 10, h = a % 100; return a + " " + (k === 1 && h !== 11 ? one : k >= 2 && k <= 4 && (h < 10 || h >= 20) ? few : many); };
    const list = (a) => a.slice(0, 4).map(esc).join(", ") + (a.length > 4 ? ` и ещё ${a.length - 4}` : "");
    const rows = [];
    if (r.redownloaded?.length) rows.push(`🔁 Скачано заново (были повреждены или пропали): <b>${n(r.redownloaded.length, "файл", "файла", "файлов")}</b> <span class="muted">— ${list(r.redownloaded)}</span>`);
    if (r.restored?.length) rows.push(`➕ Возвращены файлы сборки: <b>${n(r.restored.length, "файл", "файла", "файлов")}</b> <span class="muted">— ${list(r.restored)}</span>`);
    if (r.removed?.length) rows.push(`🗑 Убраны лишние моды: <b>${list(r.removed)}</b> <span class="muted">(лежат в папке mods-removed)</span>`);
    if (r.broken_mods?.length) rows.push(`⚠️ Повреждены моды: <b>${list(r.broken_mods)}</b> <span class="muted">— скачайте их заново или удалите</span>`);
    if (r.java) rows.push(`☕ ${esc(r.java)}`);
    if (r.pack_updated) rows.push(`⬆️ Сборка обновлена до последней версии`);
    const ok = !rows.length;
    rows.push(ok ? "✅ Все файлы в порядке — чинить ничего не пришлось" : "✅ Остальное в порядке");
    $("#repTitle").textContent = ok ? "Всё в порядке ✅" : "Сборка починена ✅";
    $("#repBody").innerHTML = `<p>Проверено <b>${n(r.checked, "файл", "файла", "файлов")}</b>.${ok ? "" : " Что исправлено:"}</p>
      <div class="replist">${rows.map(x => `<div>${x}</div>`).join("")}</div>`;
    $("#repFoot").innerHTML = `<button class="ghost" id="repFolder">Открыть папку</button><button class="play small" id="repPlay">▶ Играть</button>`;
    $("#repFolder").onclick = () => api("OpenFolder", i.id);
    $("#repPlay").onclick = () => { $("#repDlg").classList.remove("on"); select(i.id).then(() => play(i.id, !!(i.server || isAdv(i) || i.kind === "pack"))); };
    $("#repDlg").classList.add("on");
  } catch (e) {
    clearInterval(tick);
    $("#repTitle").textContent = "Не получилось";
    $("#repBody").innerHTML = `<p class="err">${esc(e.message)}</p>`;
    $("#repFoot").innerHTML = `<button class="ghost" data-x>Закрыть</button>`;
    $("#repFoot [data-x]").onclick = () => $("#repDlg").classList.remove("on");
    $("#repDlg").classList.add("on");
  }
}
$("#eRam").oninput = () => $("#eRamV").textContent = $("#eRam").value + " ГБ";
$("#eOk").onclick = async () => {
  try {
    if (!$("#eLoaderBox").hidden && (EL.loader !== (E.loader || "vanilla") || EL.lv !== (E.loader_version || ""))) {
      await api("SetLoader", E.id, EL.loader, EL.lv);
    }
    await api("Update", E.id, $("#eName").value, $("#eServer").value, +$("#eRam").value * 1024);
    $("#editDlg").classList.remove("on"); refresh();
  }
  catch (e) { $("#eErr").textContent = e.message; }
};
$("#eDel").onclick = async () => {
  const msg = isAdv(E) ? "Убрать сборку из списка? Файлы игры останутся на месте." : `Удалить «${E.name}» вместе с мирами и модами? Это нельзя отменить.`;
  if (!confirm(msg)) return;
  try { await api("Delete", E.id); $("#editDlg").classList.remove("on"); refresh(); }
  catch (e) { $("#eErr").textContent = e.message; }
};

// ---------------------------------------------------------------- settings
$("#saveSettings").onclick = async () => {
  try { await api("SaveSettings", $("#nick").value, $("#skin").value); await refresh(); alertOk("Настройки сохранены"); }
  catch (e) { alertBar(e.message); }
};
function alertOk(msg) { $("#status").textContent = msg; $("#status").classList.remove("err"); }

$("#saveKeys").onclick = async () => {
  try { await api("SetCFKey", $("#cfkey").value); alertOk("Ключ сохранён"); refresh(); }
  catch (e) { alertBar(e.message); }
};

// ---------------------------------------------------------------- import
$("#impBtn").onclick = () => $("#impFile").click();
$("#impFile").onchange = async () => {
  const f = $("#impFile").files[0]; $("#impFile").value = "";
  if (!f) return;
  $("#impTitle").textContent = "Импорт: " + f.name;
  $("#impBody").innerHTML = `<p class="muted">Загружаем файл и скачиваем моды — прогресс внизу окна…</p>`;
  $("#impDlg").classList.add("on");
  try {
    const up = await (await fetch("/upload", {method: "POST", headers: {"X-Token": TOKEN}, body: f})).json();
    if (up.error) throw new Error(up.error);
    refresh();
    const res = await job("ImportPack", up.result);
    let html = `<p>Сборка <b>${esc(res.name)}</b> создана, модов скачано: ${res.mods}.</p><p class="muted">Нажмите «Играть» — лаунчер поставит нужные Minecraft и загрузчик.</p>`;
    if (res.manual?.length) html += `<p style="margin-top:6px">Эти моды CurseForge не даёт скачать автоматически${S.settings.cf_key ? "" : " (нет ключа API в «Настройках»)"} — скачайте их и положите в папку <b>mods</b> сборки:</p>
      <div class="manual">${res.manual.map(m => `<button data-url="${esc(m.url)}">${esc(m.name)} ↗</button>`).join("")}</div>`;
    $("#impBody").innerHTML = html;
    $$("#impBody [data-url]").forEach(b => b.onclick = () => api("OpenURL", b.dataset.url));
    await refresh(); await select(res.id);
  } catch (e) { $("#impBody").innerHTML = `<p class="err">${esc(e.message)}</p>`; refresh(); }
};

// ---------------------------------------------------------------- loop
function renderUpdate() {
  const u = S && S.update;
  $("#updBar").hidden = !u;
  if (!u) return;
  $("#updVer").textContent = fmtVer(u.version);
  $("#updNotes").textContent = u.notes ? " — " + u.notes : "";
  $("#updBtn").disabled = !!S.busy || !!S.running;
  $("#updBtn").title = S.running ? "Закройте игру, чтобы обновить" : "";
}
$("#updBtn").onclick = async () => {
  try { $("#updBtn").disabled = true; await job("SelfUpdate"); }
  catch (e) { alertBar(e.message); $("#updBtn").disabled = false; }
};
async function refresh() {
  try { S = await api("State"); render(); renderUpdate(); } catch (e) { alertBar("Нет связи с лаунчером: " + e.message); }
}
refresh().then(() => { loadServers(); if (S && !S.settings.nick) show("settings"); });
setInterval(loadServers, 15000);
setTimeout(loadServers, 4500);
setInterval(() => { if (S && (S.busy || S.running)) refresh(); }, 400);
setInterval(refresh, 4000);
document.addEventListener("contextmenu", e => { if (e.target.tagName !== "INPUT") e.preventDefault(); });
