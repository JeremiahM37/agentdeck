// A full-screen chat owns its transcript; scrolling xterm's empty buffer cannot
// move it. Route touch drags through xterm's negotiated mouse protocol instead.
export function installTerminalScroll({host, term, enabled, retainedHistory, liveIntent}) {
  let gesture, momentum, disposed = false;
  const linePixels = () => Math.max(8, (term.options.fontSize || 15) * 0.8);
  // tmux itself uses the alternate screen even for a plain shell. Only a
  // negotiated mouse protocol establishes that the application owns scrolling.
  const appScroll = () => term.modes.mouseTrackingMode !== 'none';
  const stop = () => { cancelAnimationFrame(momentum); momentum = null; };
  function scroll(lines, x, y) {
    if (!lines || !enabled()) return;
    if (lines > 0) liveIntent();
    if (appScroll()) {
      const viewport = host.querySelector('.xterm-viewport');
      for (let i = 0; i < Math.min(30, Math.abs(lines)); i++) {
        viewport.dispatchEvent(new WheelEvent('wheel', {
          deltaY: Math.sign(lines) * 120, bubbles:true, cancelable:true,
          clientX:x, clientY:y,
        }));
      }
    } else if (lines < 0 && term.buffer.active.viewportY === 0) {
      stop(); retainedHistory(lines);
    } else term.scrollLines(lines);
  }
  const controller = new AbortController();
  const listen = (type, fn, options = {}) => host.addEventListener(type, fn, {...options, signal:controller.signal});
  listen('wheel', (e) => {
    if (e.deltaY > 0 && enabled()) liveIntent();
    // Native mouse/trackpad scrolling already works inside mouse-driven apps.
    // At the top of a plain terminal, fetch history from before this attachment.
    if (e.ctrlKey || e.deltaY >= 0 || !enabled() || appScroll() || term.buffer.active.viewportY !== 0) return;
    e.preventDefault(); e.stopPropagation();
    retainedHistory(-Math.max(3, Math.round(Math.abs(e.deltaY) / linePixels())));
  }, {capture:true, passive:false});
  listen('pointerdown', (e) => {
    if (e.pointerType !== 'touch' || !e.isPrimary || !enabled()) return;
    stop();
    gesture = {id:e.pointerId, y:e.clientY, x:e.clientX, start:e.clientY,
      time:performance.now(), remainder:0, velocity:0, dragged:false};
  }, {capture:true});
  listen('pointermove', (e) => {
    if (!gesture || e.pointerId !== gesture.id) return;
    const g = gesture, now = performance.now();
    if (!g.dragged && Math.abs(e.clientY - g.start) < 6) return;
    g.dragged = true;
    try { host.setPointerCapture(e.pointerId); } catch {}
    const delta = g.y - e.clientY;
    g.velocity = delta / Math.max(1, now - g.time);
    g.remainder += delta;
    const lines = Math.trunc(g.remainder / linePixels());
    g.remainder -= lines * linePixels();
    g.y = e.clientY; g.x = e.clientX; g.time = now;
    e.preventDefault(); e.stopPropagation();
    scroll(lines, g.x, g.y);
  }, {capture:true, passive:false});
  // Prevent xterm's native touch handler from consuming the same gesture twice.
  listen('touchmove', (e) => {
    if (gesture?.dragged) { e.preventDefault(); e.stopPropagation(); }
  }, {capture:true, passive:false});
  function end(e) {
    if (!gesture || e.pointerId !== gesture.id) return;
    const g = gesture; gesture = null;
    try { host.releasePointerCapture(e.pointerId); } catch {}
    if (!g.dragged || e.type === 'pointercancel' || performance.now()-g.time > 100) return;
    e.preventDefault(); e.stopPropagation();
    let last = performance.now(), velocity = g.velocity, remainder = 0;
    const step = (now) => {
      if (disposed || !enabled()) return;
      const dt = Math.min(40, now-last); last = now;
      velocity *= Math.pow(0.90, dt/16);
      if (Math.abs(velocity) < 0.03) return;
      remainder += velocity * dt;
      const lines = Math.trunc(remainder / linePixels()); remainder -= lines * linePixels();
      scroll(lines, g.x, g.y);
      momentum = requestAnimationFrame(step);
    };
    momentum = requestAnimationFrame(step);
  }
  listen('pointerup', end, {capture:true});
  listen('pointercancel', end, {capture:true});
  return () => { disposed = true; stop(); controller.abort(); };
}
