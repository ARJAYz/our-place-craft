// Industrial Upgrade: дополнительные жилы бура Create Ore Excavation (22.11.2026).
// Руды IU в обычном мире отключены (kubejs/data/industrialupgrade/neoforge/biome_modifier),
// поэтому материалы без аналогов в других модах добываются бурами из этих жил.
ServerEvents.recipes(event => {
  const R = [
    ["opc_iu:coe/drilling_gems", {"type": "createoreexcavation:drilling", "drill": {"tag": "createoreexcavation:drills"}, "output": [{"id": "industrialupgrade:preciousgem/ruby_gem"}, {"chance": 0.5, "id": "industrialupgrade:preciousgem/sapphire_gem"}, {"chance": 0.5, "id": "industrialupgrade:preciousgem/topaz_gem"}], "priority": 0, "stress": 512, "ticks": 600, "veinId": "opc_iu:coe/vein_gems"}],
    ["opc_iu:coe/vein_gems", {"type": "createoreexcavation:vein", "amountMultiplierMax": 30.0, "amountMultiplierMin": 10.0, "biomeWhitelist": "minecraft:is_overworld", "finite": "default", "icon": {"count": 1, "id": "industrialupgrade:preciousgem/ruby_gem"}, "name": "{\"text\": \"Жила самоцветов\"}", "placement": {"salt": 70028, "separation": 8, "spacing": 160}, "priority": 0}],
    ["opc_iu:coe/drilling_light", {"type": "createoreexcavation:drilling", "drill": {"tag": "createoreexcavation:drills"}, "output": [{"id": "industrialupgrade:baseore1/lithium"}, {"chance": 0.3, "id": "industrialupgrade:baseore1/bor"}, {"chance": 0.2, "id": "industrialupgrade:baseore1/beryllium"}], "priority": 0, "stress": 512, "ticks": 600, "veinId": "opc_iu:coe/vein_light"}],
    ["opc_iu:coe/vein_light", {"type": "createoreexcavation:vein", "amountMultiplierMax": 30.0, "amountMultiplierMin": 10.0, "biomeWhitelist": "minecraft:is_overworld", "finite": "default", "icon": {"count": 1, "id": "industrialupgrade:baseore1/lithium"}, "name": "{\"text\": \"Жила лёгких металлов\"}", "placement": {"salt": 70025, "separation": 8, "spacing": 160}, "priority": 0}],
    ["opc_iu:coe/drilling_sulfur", {"type": "createoreexcavation:drilling", "drill": {"tag": "createoreexcavation:drills"}, "output": [{"id": "industrialupgrade:baseore2/sulfur"}], "priority": 0, "stress": 256, "ticks": 600, "veinId": "opc_iu:coe/vein_sulfur"}],
    ["opc_iu:coe/vein_sulfur", {"type": "createoreexcavation:vein", "amountMultiplierMax": 30.0, "amountMultiplierMin": 10.0, "biomeWhitelist": "minecraft:is_overworld", "finite": "default", "icon": {"count": 1, "id": "industrialupgrade:baseore2/sulfur"}, "name": "{\"text\": \"Серная жила\"}", "placement": {"salt": 70026, "separation": 8, "spacing": 160}, "priority": 0}],
    ["opc_iu:coe/drilling_thorium", {"type": "createoreexcavation:drilling", "drill": {"tag": "createoreexcavation:drills"}, "output": [{"id": "industrialupgrade:thorium_ore/thorium_ore"}, {"chance": 0.3, "id": "industrialupgrade:classicore/uranium"}], "priority": 0, "stress": 1024, "ticks": 600, "veinId": "opc_iu:coe/vein_thorium"}],
    ["opc_iu:coe/vein_thorium", {"type": "createoreexcavation:vein", "amountMultiplierMax": 30.0, "amountMultiplierMin": 10.0, "biomeWhitelist": "minecraft:is_overworld", "finite": "default", "icon": {"count": 1, "id": "industrialupgrade:thorium_ore/thorium_ore"}, "name": "{\"text\": \"Жила тория\"}", "placement": {"salt": 70027, "separation": 8, "spacing": 160}, "priority": 0}]
  ]
  R.forEach(([id, json]) => {
    try { event.custom(json).id(id) } catch (e) { console.warn("iu_ores_extra: " + id + ": " + e) }
  })
})
