// =====================================================================
//  Прогрессия сборки: Create -> Immersive Engineering -> Mekanism ->
//  (AE2 / Ender IO / Flux) -> Extreme Reactors -> Draconic
//
//  Industrial Upgrade идёт своей веткой (справочник IU), руды и металлы общие.
//  Ключевые предметы каждого мода требуют деталей из предыдущей ступени.
//  Чтобы отключить прогрессию — удалите или переименуйте этот файл
//  (например, в progression.js.off) и выполните /reload.
// =====================================================================

ServerEvents.recipes(event => {
  const id = name => `kubejs:progression/${name}`

  const replace = (output, pattern, keys, name) => {
    event.remove({ output: output })
    event.shaped(output, pattern, keys).id(id(name))
  }

  // ---------- Ступень 1: Immersive Engineering требует Create ----------
  // Молот инженера (нужен для сборки всех мультиблоков IE)
  replace('immersiveengineering:hammer',
    [' IF', ' SA', 'S  '],
    {
      I: 'create:iron_sheet',
      F: '#c:strings',
      S: '#c:rods/wooden',
      A: 'create:andesite_alloy'
    }, 'ie_hammer')

  // ---------- Ступень 2: Mekanism требует Create + Immersive Engineering ----------
  // Металлургический заполнитель — без него нет сплавов и схем Mekanism
  replace('mekanism:metallurgic_infuser',
    ['IFI', 'POP', 'ISI'],
    {
      I: '#c:ingots/iron',
      F: 'minecraft:furnace',
      P: 'create:precision_mechanism',
      O: '#c:ingots/osmium',
      S: 'immersiveengineering:component_steel'
    }, 'mek_metallurgic_infuser')

  // Стальной корпус — основа всех машин Mekanism
  replace('mekanism:steel_casing',
    ['SGS', 'BOB', 'SGS'],
    {
      S: '#c:ingots/steel',
      G: '#c:glass_blocks/cheap',
      B: 'create:brass_casing',
      O: '#c:ingots/osmium'
    }, 'mek_steel_casing')

  // ---------- Ступень 3: AE2 требует Mekanism + Create ----------
  // Высекатель — без него нет процессоров AE2
  replace('ae2:inscriber',
    ['APA', 'C S', 'APA'],
    {
      A: '#c:ingots/iron',
      P: 'create:mechanical_press',
      C: 'mekanism:basic_control_circuit',
      S: 'mekanism:steel_casing'
    }, 'ae2_inscriber')

  // ---------- Ступень 3: Ender IO требует Mekanism ----------
  // Пустой корпус — основа всех машин Ender IO
  replace('enderio:void_chassis',
    ['IGI', 'GCG', 'IGI'],
    {
      I: '#c:ingots/iron',
      G: '#c:dusts/grains_of_infinity',
      C: 'mekanism:basic_control_circuit'
    }, 'enderio_void_chassis')

  // ---------- Ступень 3: Flux Networks требует Mekanism ----------
  replace('fluxnetworks:flux_core',
    ['FAF', 'OEO', 'FAF'],
    {
      F: 'fluxnetworks:flux_dust',
      A: 'mekanism:alloy_infused',
      O: 'minecraft:obsidian',
      E: 'minecraft:ender_eye'
    }, 'flux_core')

  // ---------- Ступень 4: Extreme Reactors требует продвинутые схемы Mekanism ----------
  const reactorController = (output, casing, circuit, fuelTag, name) => {
    event.shaped(output, ['CXC', 'YDY', 'CRC'], {
      C: casing,
      X: 'minecraft:comparator',
      Y: fuelTag,
      D: circuit,
      R: '#c:dusts/redstone'
    }).id(id(name))
  }
  event.remove({ output: 'bigreactors:basic_reactorcontroller' })
  event.remove({ output: 'bigreactors:reinforced_reactorcontroller' })
  reactorController('bigreactors:basic_reactorcontroller', 'bigreactors:basic_reactorcasing',
    'mekanism:advanced_control_circuit', '#c:ingots/yellorium', 'er_basic_reactor_controller_yellorium')
  reactorController('bigreactors:basic_reactorcontroller', 'bigreactors:basic_reactorcasing',
    'mekanism:advanced_control_circuit', '#c:ingots/uranium', 'er_basic_reactor_controller_uranium')
  reactorController('bigreactors:reinforced_reactorcontroller', 'bigreactors:reinforced_reactorcasing',
    'mekanism:elite_control_circuit', '#c:ingots/yellorium', 'er_reinforced_reactor_controller_yellorium')
  reactorController('bigreactors:reinforced_reactorcontroller', 'bigreactors:reinforced_reactorcasing',
    'mekanism:elite_control_circuit', '#c:ingots/uranium', 'er_reinforced_reactor_controller_uranium')

  event.remove({ output: 'bigreactors:basic_turbinecontroller' })
  event.remove({ output: 'bigreactors:reinforced_turbinecontroller' })
  reactorController('bigreactors:basic_turbinecontroller', 'bigreactors:basic_turbinecasing',
    'mekanism:advanced_control_circuit', '#c:storage_blocks/cyanite', 'er_basic_turbine_controller')
  reactorController('bigreactors:reinforced_turbinecontroller', 'bigreactors:reinforced_turbinecasing',
    'mekanism:elite_control_circuit', '#c:storage_blocks/cyanite', 'er_reinforced_turbine_controller')

  // ---------- Ступень 5: Draconic Evolution требует AE2 + Mekanism ----------
  // Ядро синтеза — без него нет синтез-крафтов Draconic
  replace('draconicevolution:crafting_core',
    ['LPL', 'ECE', 'LPL'],
    {
      L: '#c:storage_blocks/lapis',
      P: 'ae2:engineering_processor',
      E: 'mekanism:elite_control_circuit',
      C: 'draconicevolution:draconium_core'
    }, 'de_crafting_core')
})
