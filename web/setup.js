"use strict";
const TOKEN = location.hash.slice(1);
const $ = s => document.querySelector(s);
const $$ = s => [...document.querySelectorAll(s)];
async function api(method, ...args) {
  const r = await fetch("/api/" + method, {method: "POST", headers: {"X-Token": TOKEN}, body: JSON.stringify(args)});
  const j = await r.json();
  if (j.error) throw new Error(j.error);
  return j.result;
}
const opt = {adventure: true, desktop: true, clean: false, migrate: true};
let info = {};

function setOpt(name, on) {
  opt[name] = on;
  const b = $(`[data-opt=${name}]`);
  b.classList.toggle("on", on);
  b.querySelector(".sw").classList.toggle("on", on);
  $("#codeRow").hidden = !(opt.adventure && !info.stamped && !info.legacy);
  if (info.installed) $("#go").textContent = opt.clean ? "ПЕРЕУСТАНОВИТЬ" : "ОБНОВИТЬ ЛАУНЧЕР";
  $("#go").classList.toggle("red", opt.clean);
}
$$("[data-opt]").forEach(b => b.onclick = () => { if (!b.classList.contains("dis")) setOpt(b.dataset.opt, !opt[b.dataset.opt]); });

$("#pick").onclick = async () => { try { $("#root").value = await api("PickFolder"); checkRoot(); } catch (e) { $("#err").textContent = e.message; } };
function checkRoot() {
  const bad = /[^\x00-\x7f]/.test($("#root").value);
  $("#rootHint").textContent = bad ? "В пути есть русские буквы — Java с таким путём не запустится. Выберите папку с латинским путём." :
    "Сюда же лягут сборки, Java и миры. Путь — только латинскими буквами.";
  $("#rootHint").classList.toggle("err", bad);
}
$("#root").oninput = checkRoot;

$("#go").onclick = async () => {
  $("#err").textContent = "";
  const o = {root: $("#root").value.trim(), nick: $("#nick").value.trim(), desktop: opt.desktop,
    adventure: opt.adventure && !info.legacy, code: $("#code").value.trim(), clean: opt.clean && info.installed,
    migrate: !!info.legacy && opt.migrate};
  if (!/^[A-Za-z0-9_]{3,16}$/.test(o.nick)) { $("#err").textContent = "Ник: от 3 до 16 символов — латиница, цифры и _"; return; }
  if (o.adventure && !info.stamped && !o.code) { $("#err").textContent = "Введите код доступа к сборке или снимите галочку"; return; }
  $("#stepForm").hidden = true; $("#stepWork").hidden = false;
  $("#workTitle").textContent = o.clean ? "Переустанавливаем…" : (info.installed ? "Обновляем…" : "Устанавливаем…");
  let id;
  try { id = await api("Install", o); } catch (e) { back(e.message); return; }
  for (;;) {
    await new Promise(r => setTimeout(r, 300));
    const s = await api("State");
    $("#workStage").textContent = s.stage || s.status;
    $("#sprog").classList.toggle("ind", !(s.total > 0));
    $("#sbar").style.width = s.total > 0 ? Math.min(100, s.done * 100 / s.total) + "%" : "";
    $("#workBytes").textContent = s.total > 4 << 20 ? `${mb(s.done)} из ${mb(s.total)}` : "";
    const j = await api("Job", id);
    if (!j.done) continue;
    if (j.error && !j.error.startsWith("лаунчер установлен")) { back(j.error); return; }
    $("#stepWork").hidden = true; $("#stepDone").hidden = false;
    $("#doneText").textContent = o.adventure
      ? "Лаунчер и сборка Our Place Craft: Adventure установлены. Нажмите «Играть» на главной."
      : (o.migrate ? "Готово: сборка, миры и настройки перенесены в новый лаунчер, старый лаунчер удалён."
        : (info.legacy ? "Лаунчер установлен. Ваша сборка Adventure из " + info.legacy + " уже в списке." : "Лаунчер установлен."));
    if (o.clean) { const si = await api("SetupInfo"); if (si.backup) $("#doneText").textContent += " Ваши миры сохранены в папке " + si.backup + "."; }
    $("#doneWarn").textContent = j.error || "";
    return;
  }
};
const mb = n => n >= 1 << 30 ? (n / (1 << 30)).toFixed(1) + " ГБ" : Math.round(n / (1 << 20)) + " МБ";
function back(msg) { $("#stepWork").hidden = true; $("#stepForm").hidden = false; $("#err").textContent = msg; }
$("#start").onclick = () => api("StartLauncher").catch(e => $("#doneWarn").textContent = e.message);

(async () => {
  info = await api("SetupInfo");
  $("#root").value = info.root; $("#nick").value = info.nick || "";
  if (info.installed) {
    $("#go").textContent = "ОБНОВИТЬ ЛАУНЧЕР"; document.querySelector("#stepForm h2").textContent = "Обновление лаунчера";
    $("[data-opt=clean]").hidden = false;
  }
  if (info.legacy) {
    const b = $("[data-opt=adventure]"); b.classList.add("dis");
    $("#advNote").textContent = "Уже стоит в старом лаунчере (" + info.legacy + ") — скачивать заново не нужно.";
    setOpt("adventure", false);
    $("[data-opt=migrate]").hidden = false;
  } else setOpt("adventure", true);
  checkRoot();
})();
document.addEventListener("contextmenu", e => { if (e.target.tagName !== "INPUT") e.preventDefault(); });
