// Выдаёт игроку книгу Akashic Tome со всеми руководствами:
//  - один раз при первом входе в мир;
//  - снова после смерти, если тома в инвентаре нет.
//
// v5 (08.11.2026): каждая книга проверяется перед тем, как попасть в том.
// Раньше одна запись могла оказаться «пустой» (0 minecraft:air) — такой том сервер
// не может отправить игроку и выкидывает его с сервера сразу после возрождения
// (EncoderException: Empty ItemStack not allowed).

var GUIDE_BOOKS = [
  ['ftbquests', 'ftbquests:book', ''],
  ['create', 'patchouli:guide_book', '"patchouli:book":"patchouli:create_guide"'],
  ['bigreactors', 'patchouli:guide_book', '"patchouli:book":"bigreactors:erguide"'],
  ['industrialupgrade', 'industrialupgrade:book/guide_book', ''],
  ['laserio', 'patchouli:guide_book', '"patchouli:book":"laserio:laseriobook"'],
  ['productivebees', 'patchouli:guide_book', '"patchouli:book":"productivebees:guide"'],
  ['productivetrees', 'patchouli:guide_book', '"patchouli:book":"productivetrees:guide"'],
  ['immersiveengineering', 'immersiveengineering:manual', ''],
  ['ae2', 'ae2:guide', ''],
  ['herbsandharvest', 'herbsandharvest:guide_book', '']
]

// true, если такой предмет есть в игре (иначе Item.of даёт пустой стак)
function itemExists(id) {
  try {
    var st = Item.of(id)
    return st != null && !st.isEmpty() && String(st.id) === id
  } catch (e) {
    return false
  }
}

function buildGuideEntry(b) {
  var comps = ['"akashictome:defined_mod":"' + b[0] + '"']
  if (b[2]) comps.unshift(b[2])
  return '{id:"' + b[1] + '",count:1,components:{' + comps.join(',') + '}}'
}

// Проверяет готовый том: ни одной пустой книги внутри
function tomeIsHealthy(tome) {
  try {
    var s = String(tome)
    if (tome.isEmpty()) return false
    var c = tome.getComponentsPatch ? String(tome.getComponentsPatch()) : ''
    return c.indexOf('minecraft:air') < 0 && s.indexOf('minecraft:air') < 0
  } catch (e) {
    return true // проверить не получилось — не мешаем выдаче
  }
}

function buildTome() {
  var entries = []
  for (var i = 0; i < GUIDE_BOOKS.length; i++) {
    var b = GUIDE_BOOKS[i]
    if (!itemExists(b[1])) {
      console.warn('give_guides: предмета ' + b[1] + ' нет в игре — книга ' + b[0] + ' не попадёт в том')
      continue
    }
    entries.push(buildGuideEntry(b))
  }
  var tome = Item.of('akashictome:tome[akashictome:tool_content=[' + entries.join(',') + ']]')
  if (tomeIsHealthy(tome)) return tome
  console.error('give_guides: том получился с пустой книгой — выдаём пустой том')
  return Item.of('akashictome:tome')
}

function hasTome(player) {
  try {
    var inv = player.inventory
    for (var i = 0; i < inv.getContainerSize(); i++) {
      var st = inv.getItem(i)
      if (st && !st.isEmpty() && String(st.id) === 'akashictome:tome') return true
    }
  } catch (e) {}
  return false
}

function giveTome(player) {
  try {
    player.give(buildTome())
  } catch (e) {
    console.error('give_guides: не удалось выдать Tome с книгами: ' + e)
    player.give(Item.of('akashictome:tome'))
  }
}

// Убирает отдельные книги, которые выдают сами моды (они уже лежат внутри Tome)
function removeStandaloneGuides(player) {
  try {
    var inv = player.inventory
    var n = inv.getContainerSize()
    for (var i = 0; i < n; i++) {
      var st = inv.getItem(i)
      if (st && String(st.id) === 'herbsandharvest:guide_book') {
        inv.setItem(i, Item.of('minecraft:air'))
      }
    }
  } catch (e) {
    console.error('give_guides: не удалось убрать лишнюю книгу: ' + e)
  }
}

function scheduleCleanup(event, player) {
  player.persistentData.putBoolean('herbsandharvest_book_given', true)
  removeStandaloneGuides(player)
  event.server.scheduleInTicks(20, function () { removeStandaloneGuides(player) })
  event.server.scheduleInTicks(100, function () { removeStandaloneGuides(player) })
  event.server.scheduleInTicks(300, function () { removeStandaloneGuides(player) })
}

PlayerEvents.loggedIn(function (event) {
  var player = event.player
  if (player.persistentData.getBoolean('guides_given_v4')) {
    scheduleCleanup(event, player)
    return
  }
  player.persistentData.putBoolean('guides_given_v4', true)
  giveTome(player)
  player.tell(Text.green('Вам выдана книга-справочник со всеми руководствами сборки.'))
  scheduleCleanup(event, player)
})

// После смерти: выдаём книгу заново (выход из Энда смертью не считается).
// Выдаём на секунду позже — когда игрок уже точно возродился.
PlayerEvents.respawned(function (event) {
  var player = event.player
  var endConquered = false
  try { endConquered = event.endConquered } catch (e) {}
  if (endConquered) return
  event.server.scheduleInTicks(20, function () {
    if (hasTome(player)) return
    giveTome(player)
    player.tell(Text.green('Книга-справочник выдана снова.'))
  })
  scheduleCleanup(event, player)
})
