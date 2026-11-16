-- Subtitle drag support for the review preview.
--
-- mpv reports mouse down/move/up to the Electron app through script-message
-- client-message events. The app hit-tests the event boxes and replies with
-- "subsandwicher-set-box" (playRes + boxes); this script draws a translucent
-- ghost rectangle and sends the final delta on release.
--
-- The app is expected to have disabled the default mpv bindings and window
-- dragging. Chatty client-message events are not logged by mpv-ipc.js.

local mp = require 'mp'

local drag = {
  active = false,
  mode = 'normal',
  start = nil,
  playW = 0,
  playH = 0,
  boxes = {},
  dx = 0,
  dy = 0,
}

local overlay = mp.create_osd_overlay('ass-events')
overlay.z = 2000

local function video_rect()
  local d = mp.get_property_native('osd-dimensions')
  if not d then return nil end
  local w = (d.w or 0) - (d.ml or 0) - (d.mr or 0)
  local h = (d.h or 0) - (d.mt or 0) - (d.mb or 0)
  if w <= 0 or h <= 0 then return nil end
  return d, w, h
end

local function send(phase, x, y, time, dx, dy)
  mp.commandv('script-message', 'subsandwicher-drag', phase,
    tostring(x or ''), tostring(y or ''), tostring(drag.mode or 'normal'),
    tostring(time or ''), tostring(dx or ''), tostring(dy or ''))
end

local function ghost_data()
  local lines = {}
  for _, b in ipairs(drag.boxes) do
    local x0 = b.x0 + drag.dx
    local y0 = b.y0 + drag.dy
    local w = b.x1 - b.x0
    local h = b.y1 - b.y0
    -- A green translucent rectangle with a dark outline.
    lines[#lines + 1] = string.format(
      '{\\an7\\pos(%.2f,%.2f)\\p1\\bord2\\shad0\\1c&H00FF00&\\1a&H70&\\3c&H000000&}m 0 0 l %.2f 0 %.2f %.2f 0 %.2f{\\p0}',
      x0, y0, w, w, h, h)
  end
  return table.concat(lines, '\n')
end

local function update_ghost()
  if drag.playW <= 0 or drag.playH <= 0 or #drag.boxes == 0 then
    overlay:remove()
    return
  end
  overlay.res_x = drag.playW
  overlay.res_y = drag.playH
  overlay.data = ghost_data()
  overlay:update()
end

-- Convert the current mouse position to a delta from the drag start, in
-- PlayRes coordinates. Shift locks the dominant axis; Alt scales the delta.
local function current_delta()
  if not drag.start then return 0, 0 end
  local d, w, h = video_rect()
  local m = mp.get_property_native('mouse-pos')
  if not d or not m or drag.playW <= 0 or drag.playH <= 0 then return 0, 0 end
  local dx = (m.x - drag.start.x) / w * drag.playW
  local dy = (m.y - drag.start.y) / h * drag.playH
  if drag.mode == 'axis' then
    if math.abs(dx) >= math.abs(dy) then
      dy = 0
    else
      dx = 0
    end
  elseif drag.mode == 'fine' then
    dx = dx * 0.25
    dy = dy * 0.25
  end
  return dx, dy
end

local function update_drag()
  drag.dx, drag.dy = current_delta()
  update_ghost()
end

local function begin_drag(mode)
  return function(e)
    if e.canceled then
      return
    end
    if e.event == 'down' then
      if drag.active then return end
      local d, w, h = video_rect()
      local m = mp.get_property_native('mouse-pos')
      if not d or not m then return end
      local key = string.lower(e.key_name or '')
      local effective = mode
      if key:find('shift', 1, true) then
        effective = 'axis'
      elseif key:find('alt', 1, true) then
        effective = 'fine'
      end
      drag.active = true
      drag.mode = effective
      drag.start = { x = m.x, y = m.y }
      drag.playW = 0
      drag.playH = 0
      drag.boxes = {}
      drag.dx = 0
      drag.dy = 0
      overlay:remove()
      local nx = (m.x - (d.ml or 0)) / w
      local ny = (m.y - (d.mt or 0)) / h
      send('down', nx, ny, mp.get_property_number('time-pos'))
    elseif e.event == 'up' then
      if not drag.active then return end
      update_drag()
      local dx, dy = drag.dx, drag.dy
      drag.active = false
      drag.boxes = {}
      overlay:remove()
      send('up', 0, 0, mp.get_property_number('time-pos'), dx, dy)
    end
    -- repeat is intentionally ignored
  end
end

-- The app needs to know whether the click hit a subtitle. If it does, it sends
-- back the PlayRes and one or more boxes to animate as a ghost.
mp.register_script_message('subsandwicher-set-box', function(playW, playH, ...)
  drag.playW = tonumber(playW) or 0
  drag.playH = tonumber(playH) or 0
  drag.boxes = {}
  local args = { ... }
  for i = 1, #args, 4 do
    local x0 = tonumber(args[i])
    local y0 = tonumber(args[i + 1])
    local x1 = tonumber(args[i + 2])
    local y1 = tonumber(args[i + 3])
    if x0 and y0 and x1 and y1 and x1 > x0 and y1 > y0 then
      drag.boxes[#drag.boxes + 1] = { x0 = x0, y0 = y0, x1 = x1, y1 = y1 }
    end
  end
  if drag.active then
    update_drag()
  else
    overlay:remove()
  end
end)

mp.register_script_message('subsandwicher-cancel', function()
  drag.active = false
  drag.boxes = {}
  overlay:remove()
end)

local function bind(key, name, fn, opts)
  -- One unsupported modifier should not abort the whole script.
  pcall(mp.add_forced_key_binding, key, name, fn, opts)
end

bind('MOUSE_MOVE', 'subsandwicher-drag-move', function()
  if not drag.active then return end
  update_drag()
  send('move', 0, 0, mp.get_property_number('time-pos'), drag.dx, drag.dy)
end)

bind('MBTN_LEFT', 'subsandwicher-drag-normal', begin_drag('normal'), { complex = true })
bind('Shift+MBTN_LEFT', 'subsandwicher-drag-axis', begin_drag('axis'), { complex = true })
bind('Alt+MBTN_LEFT', 'subsandwicher-drag-fine', begin_drag('fine'), { complex = true })

bind('ESC', 'subsandwicher-drag-cancel', function()
  overlay:remove()
  drag.active = false
  drag.boxes = {}
  send('cancel', 0, 0, mp.get_property_number('time-pos'))
end)

-- Reset the selected event to the unfixed original.
bind('R', 'subsandwicher-reset', function()
  send('reset', 0, 0, mp.get_property_number('time-pos'))
end)

-- Arrow nudging. Normal = 1 PlayRes pixel, Shift = 10, Alt = 0.5.
local nudges = {
  { 'LEFT', -1, 0 }, { 'RIGHT', 1, 0 }, { 'UP', 0, -1 }, { 'DOWN', 0, 1 },
  { 'Shift+LEFT', -10, 0 }, { 'Shift+RIGHT', 10, 0 },
  { 'Shift+UP', 0, -10 }, { 'Shift+DOWN', 0, 10 },
  { 'Alt+LEFT', -0.5, 0 }, { 'Alt+RIGHT', 0.5, 0 },
  { 'Alt+UP', 0, -0.5 }, { 'Alt+DOWN', 0, 0.5 },
}
for i, n in ipairs(nudges) do
  local key, dx, dy = n[1], n[2], n[3]
  bind(key, 'subsandwicher-nudge-' .. i, function()
    send('nudge', 0, 0, mp.get_property_number('time-pos'), dx, dy)
  end)
end

mp.register_event('shutdown', function()
  overlay:remove()
end)
