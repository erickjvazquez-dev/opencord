// Standard `:shortcode:` → unicode emoji map (Discord/GitHub "gemoji" names).
//
// Discord renders STANDARD unicode emoji from shortcodes — `:joy:` → 😂, `:fire:` → 🔥,
// `:thumbsup:` → 👍 — in every channel and DM, and offers them in the `:`-autocomplete.
// This is the data behind Opencord's parity for that: a VENDORED, static map with NO
// runtime dependency (Rule A — must work fully offline / self-hosted; no npm fetch, no
// CDN). It is a curated, generous COMMON set (the shortcodes people actually type in
// chat — faces, gestures, hearts, food, animals, symbols, objects), not the full ~1800
// Unicode list: no offline dataset was available to vendor the complete table, and the
// common set covers the overwhelming majority of real usage. Expanding it from an
// offline generator is a logged follow-up (qa/IMPROVEMENTS.md).
//
// Small enough (a few KB) to STATIC-import: the markdown render is synchronous, so a
// lazy `import()` would flash a literal `:joy:` until the chunk resolved. Custom server
// emoji take PRECEDENCE over these in both render (markdown.tsx) and autocomplete
// (Chat.tsx) — a server may override `:fire:` with its own uploaded image, matching
// Discord. The names use the same [a-z0-9_] charset as the backend's ValidEmojiName and
// the `:slug:` markdown rule, so every key here is reachable by both render and the
// composer. Values are plain unicode strings → React-escaped on render, never injectable.

const RAW: Record<string, string> = {
  // — Smileys & emotion —
  grinning: '😀', smiley: '😃', smile: '😄', grin: '😁', laughing: '😆',
  satisfied: '😆', sweat_smile: '😅', rofl: '🤣', joy: '😂',
  slightly_smiling_face: '🙂', upside_down_face: '🙃', wink: '😉', blush: '😊',
  innocent: '😇', smiling_face_with_three_hearts: '🥰', heart_eyes: '😍',
  star_struck: '🤩', kissing_heart: '😘', kissing: '😗', relaxed: '☺️',
  kissing_closed_eyes: '😚', kissing_smiling_eyes: '😙', yum: '😋',
  stuck_out_tongue: '😛', stuck_out_tongue_winking_eye: '😜', zany_face: '🤪',
  stuck_out_tongue_closed_eyes: '😝', money_mouth_face: '🤑', hugs: '🤗',
  hand_over_mouth: '🤭', shushing_face: '🤫', thinking: '🤔',
  zipper_mouth_face: '🤐', raised_eyebrow: '🤨', neutral_face: '😐',
  expressionless: '😑', no_mouth: '😶', smirk: '😏', unamused: '😒',
  roll_eyes: '🙄', grimacing: '😬', lying_face: '🤥', relieved: '😌',
  pensive: '😔', sleepy: '😪', drooling_face: '🤤', sleeping: '😴', mask: '😷',
  face_with_thermometer: '🤒', face_with_head_bandage: '🤕', nauseated_face: '🤢',
  vomiting: '🤮', sneezing_face: '🤧', hot_face: '🥵', cold_face: '🥶',
  woozy_face: '🥴', dizzy_face: '😵', exploding_head: '🤯', cowboy_hat_face: '🤠',
  partying_face: '🥳', sunglasses: '😎', nerd_face: '🤓', monocle_face: '🧐',
  confused: '😕', worried: '😟', slightly_frowning_face: '🙁', frowning_face: '☹️',
  open_mouth: '😮', hushed: '😯', astonished: '😲', flushed: '😳',
  pleading_face: '🥺', frowning: '😦', anguished: '😧', fearful: '😨',
  cold_sweat: '😰', disappointed_relieved: '😥', cry: '😢', sob: '😭',
  scream: '😱', confounded: '😖', persevere: '😣', disappointed: '😞',
  sweat: '😓', weary: '😩', tired_face: '😫', yawning_face: '🥱', triumph: '😤',
  rage: '😡', pout: '😡', angry: '😠', cursing_face: '🤬', smiling_imp: '😈',
  imp: '👿', skull: '💀', skull_and_crossbones: '☠️', poop: '💩', hankey: '💩',
  shit: '💩', clown_face: '🤡', ghost: '👻', alien: '👽', space_invader: '👾',
  robot: '🤖', jack_o_lantern: '🎃',

  // — Gestures & body —
  wave: '👋', raised_back_of_hand: '🤚', raised_hand: '✋', hand: '✋',
  vulcan_salute: '🖖', ok_hand: '👌', pinching_hand: '🤏', v: '✌️',
  crossed_fingers: '🤞', love_you_gesture: '🤟', metal: '🤘', call_me_hand: '🤙',
  point_left: '👈', point_right: '👉', point_up_2: '👆', middle_finger: '🖕',
  point_down: '👇', point_up: '☝️', thumbsup: '👍', thumbsdown: '👎',
  fist_raised: '✊', fist: '✊', fist_oncoming: '👊', facepunch: '👊', punch: '👊',
  fist_left: '🤛', fist_right: '🤜', clap: '👏', raised_hands: '🙌',
  open_hands: '👐', palms_up_together: '🤲', handshake: '🤝', pray: '🙏',
  writing_hand: '✍️', nail_care: '💅', selfie: '🤳', muscle: '💪',
  eyes: '👀', eye: '👁️', ear: '👂', nose: '👃', tongue: '👅', lips: '👄',
  brain: '🧠', wink2: '😉',

  // — Hearts & symbols —
  heart: '❤️', orange_heart: '🧡', yellow_heart: '💛', green_heart: '💚',
  blue_heart: '💙', purple_heart: '💜', black_heart: '🖤', white_heart: '🤍',
  brown_heart: '🤎', broken_heart: '💔', heavy_heart_exclamation: '❣️',
  two_hearts: '💕', revolving_hearts: '💞', heartbeat: '💓', heartpulse: '💗',
  sparkling_heart: '💖', cupid: '💘', gift_heart: '💝', heart_decoration: '💟',
  '100': '💯', anger: '💢', boom: '💥', collision: '💥', dizzy: '💫',
  sweat_drops: '💦', dash: '💨', bomb: '💣', speech_balloon: '💬',
  thought_balloon: '💭', zzz: '💤',

  // — Nature & weather —
  fire: '🔥', star: '⭐', star2: '🌟', sparkles: '✨', zap: '⚡', snowflake: '❄️',
  sunny: '☀️', cloud: '☁️', rainbow: '🌈', umbrella: '☔', droplet: '💧',
  ocean: '🌊', moon: '🌙', crescent_moon: '🌙', earth_americas: '🌎',
  sun_with_face: '🌞', seedling: '🌱', evergreen_tree: '🌲', deciduous_tree: '🌳',
  palm_tree: '🌴', cactus: '🌵', herb: '🌿', four_leaf_clover: '🍀',
  maple_leaf: '🍁', fallen_leaf: '🍂', leaves: '🍃', mushroom: '🍄',
  bouquet: '💐', cherry_blossom: '🌸', blossom: '🌼', sunflower: '🌻',
  rose: '🌹', hibiscus: '🌺', tulip: '🌷',

  // — Celebration & objects —
  tada: '🎉', confetti_ball: '🎊', balloon: '🎈', gift: '🎁', birthday: '🎂',
  cake: '🍰', trophy: '🏆', medal_sports: '🏅', first_place_medal: '🥇',
  second_place_medal: '🥈', third_place_medal: '🥉', crown: '👑', gem: '💎',
  ring: '💍', rocket: '🚀', airplane: '✈️', car: '🚗', checkered_flag: '🏁',
  soccer: '⚽', basketball: '🏀', football: '🏈', baseball: '⚾', tennis: '🎾',
  '8ball': '🎱', video_game: '🎮', dart: '🎯', game_die: '🎲', musical_note: '🎵',
  notes: '🎶', microphone: '🎤', headphones: '🎧', guitar: '🎸',
  money_with_wings: '💸', moneybag: '💰', dollar: '💵', credit_card: '💳',
  bulb: '💡', flashlight: '🔦', battery: '🔋', computer: '💻',
  desktop_computer: '🖥️', keyboard: '⌨️', iphone: '📱', telephone: '☎️',
  camera: '📷', tv: '📺', book: '📖', books: '📚', memo: '📝', pencil2: '✏️',
  pushpin: '📌', paperclip: '📎', scissors: '✂️', lock: '🔒', unlock: '🔓',
  key: '🔑', hammer: '🔨', wrench: '🔧', gear: '⚙️', mag: '🔍', bell: '🔔',
  mega: '📣', loudspeaker: '📢', email: '✉️', envelope: '✉️', inbox_tray: '📥',
  outbox_tray: '📤', package: '📦', calendar: '📆', date: '📅', clipboard: '📋',
  bar_chart: '📊', chart_with_upwards_trend: '📈', chart_with_downwards_trend: '📉',
  hourglass: '⌛', alarm_clock: '⏰', watch: '⌚',

  // — Marks & signs —
  warning: '⚠️', no_entry: '⛔', no_entry_sign: '🚫', white_check_mark: '✅',
  heavy_check_mark: '✔️', ballot_box_with_check: '☑️', x: '❌',
  negative_squared_cross_mark: '❎', question: '❓', grey_question: '❔',
  exclamation: '❗', grey_exclamation: '❕', bangbang: '‼️', interrobang: '⁉️',
  heavy_plus_sign: '➕', heavy_minus_sign: '➖', heavy_multiplication_x: '✖️',
  heavy_division_sign: '➗', infinity: '♾️', recycle: '♻️', white_circle: '⚪',
  red_circle: '🔴', large_blue_circle: '🔵', green_circle: '🟢', yellow_circle: '🟡',
  orange_circle: '🟠', purple_circle: '🟣', brown_circle: '🟤', black_circle: '⚫',
  arrow_up: '⬆️', arrow_down: '⬇️', arrow_left: '⬅️', arrow_right: '➡️',
  repeat: '🔁', arrows_counterclockwise: '🔄', back: '🔙', soon: '🔜', top: '🔝',

  // — Food & drink —
  pizza: '🍕', hamburger: '🍔', fries: '🍟', hotdog: '🌭', taco: '🌮',
  burrito: '🌯', popcorn: '🍿', doughnut: '🍩', cookie: '🍪', chocolate_bar: '🍫',
  candy: '🍬', lollipop: '🍭', ice_cream: '🍨', icecream: '🍦', coffee: '☕',
  tea: '🍵', beer: '🍺', beers: '🍻', wine_glass: '🍷', cocktail: '🍸',
  tropical_drink: '🍹', champagne: '🍾', clinking_glasses: '🥂', tumbler_glass: '🥃',
  apple: '🍎', green_apple: '🍏', banana: '🍌', watermelon: '🍉', grapes: '🍇',
  strawberry: '🍓', peach: '🍑', cherries: '🍒', pineapple: '🍍', lemon: '🍋',
  avocado: '🥑', eggplant: '🍆', hot_pepper: '🌶️', corn: '🌽', carrot: '🥕',
  bread: '🍞', cheese: '🧀', bacon: '🥓', egg: '🥚',

  // — Animals —
  cat: '🐱', dog: '🐶', mouse: '🐭', hamster: '🐹', rabbit: '🐰', fox_face: '🦊',
  bear: '🐻', panda_face: '🐼', koala: '🐨', tiger: '🐯', lion: '🦁', cow: '🐮',
  pig: '🐷', frog: '🐸', monkey_face: '🐵', chicken: '🐔', penguin: '🐧',
  bird: '🐦', baby_chick: '🐤', duck: '🦆', eagle: '🦅', owl: '🦉', bat: '🦇',
  wolf: '🐺', horse: '🐴', unicorn: '🦄', bee: '🐝', bug: '🐛', butterfly: '🦋',
  snail: '🐌', lady_beetle: '🐞', ant: '🐜', spider: '🕷️', snake: '🐍',
  turtle: '🐢', fish: '🐟', tropical_fish: '🐠', dolphin: '🐬', whale: '🐳',
  shark: '🦈', octopus: '🐙', crab: '🦀', shrimp: '🦐', paw_prints: '🐾',
}

// The vendored map. Keys are lowercase [a-z0-9_], matching the `:slug:` render rule and
// the composer autocomplete charset, so every entry is reachable by both.
export const UNICODE_EMOJI: ReadonlyMap<string, string> = new Map(Object.entries(RAW))

// Case-insensitive lookup: returns the unicode char for a shortcode name, or undefined
// when it's not a known standard emoji (caller then leaves the `:name:` literal). The
// `:slug:` regex already lowercases, but lowercasing here keeps the helper robust for
// any caller.
export function unicodeEmoji(name: string): string | undefined {
  return UNICODE_EMOJI.get(name.toLowerCase())
}
