# A tiny demonstration of the interactive JS bridge (GOOS=js GOARCH=wasm).
#
# It reaches into the page from Ruby: grabs a <canvas id="screen">, draws a
# square, lets you drag it with the mouse, and animates a pulsing border in a
# requestAnimationFrame loop. This is the kind of primitive a Ruby-written
# window manager / compositor would build on.

doc    = JS.document
canvas = doc.call("getElementById", "screen")
ctx    = canvas.call("getContext", "2d")

box = { x: 60, y: 40, w: 120, h: 80 }
drag = nil

canvas.on("mousedown") do |e|
  mx = e.get("offsetX")
  my = e.get("offsetY")
  if mx >= box[:x] && mx <= box[:x] + box[:w] && my >= box[:y] && my <= box[:y] + box[:h]
    drag = [mx - box[:x], my - box[:y]]
  end
end

canvas.on("mousemove") do |e|
  next unless drag
  box[:x] = e.get("offsetX") - drag[0]
  box[:y] = e.get("offsetY") - drag[1]
end

canvas.on("mouseup") { |_e| drag = nil }

frame = 0
draw = nil
draw = proc do |_t|
  frame += 1
  ctx.set("fillStyle", "#0d1117")
  ctx.call("fillRect", 0, 0, canvas.get("width"), canvas.get("height"))

  ctx.set("fillStyle", "#9b1c2e")
  ctx.call("fillRect", box[:x], box[:y], box[:w], box[:h])

  ctx.set("strokeStyle", "#ffffff")
  ctx.set("lineWidth", 1 + ((Math.sin(frame / 20.0) + 1) * 2))
  ctx.call("strokeRect", box[:x], box[:y], box[:w], box[:h])

  JS.raf(&draw) # reschedule for the next frame
end

JS.raf(&draw)
