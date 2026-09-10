// Keep the shared sheet keyboard-accessible without replacing its DOM or drafts.
// Native dialogs opened above it (profiles, history, review) own their own focus.
export class SheetFocus {
  constructor(sheet) {
    this.sheet = sheet;
    this.active = false;
    this.background = new Map();
    sheet.setAttribute('role', 'dialog');
    sheet.setAttribute('aria-modal', 'true');
    sheet.tabIndex = -1;
    document.addEventListener('keydown', event => {
      if (!this.active || event.key !== 'Tab' || document.querySelector('dialog[open]')) return;
      const items = [...sheet.querySelectorAll('button,input,select,textarea,a[href],summary,[tabindex]')]
        .filter(el => !el.matches(':disabled') && el.tabIndex >= 0 && el.getClientRects().length);
      const first = items[0] || sheet, last = items.at(-1) || sheet;
      if (!items.length || !sheet.contains(document.activeElement) ||
          (event.shiftKey ? document.activeElement === first : document.activeElement === last)) {
        event.preventDefault();
        (event.shiftKey ? last : first).focus();
      }
    });
  }
  open(previous) {
    const heading = this.sheet.querySelector('h2');
    if (heading) {
      heading.id = 'sheet-title';
      this.sheet.setAttribute('aria-labelledby', heading.id);
      this.sheet.removeAttribute('aria-label');
    } else {
      this.sheet.removeAttribute('aria-labelledby');
      this.sheet.setAttribute('aria-label', 'Details');
    }
    if (this.active) return;
    this.active = true;
    this.previous = previous;
    for (const el of document.querySelectorAll('#topbar,#view,#tabbar,#terminal-workspace,#fab')) {
      this.background.set(el, el.inert);
      el.inert = true;
    }
    if (!this.sheet.contains(document.activeElement)) {
      const initial = this.sheet.querySelector('[autofocus]') ||
        this.sheet.querySelector('input:not(:disabled):not([type=hidden]),select:not(:disabled),textarea:not(:disabled)') ||
        this.sheet.querySelector('button:not(:disabled)');
      (initial || this.sheet).focus();
    }
  }
  close() {
    if (!this.active) return;
    this.active = false;
    for (const [el, inert] of this.background) el.inert = inert;
    this.background.clear();
    const prior = this.previous;
    const target = prior?.isConnected ? prior : (prior?.id && document.getElementById(prior.id));
    const usable = target && target !== document.body && !target.matches(':disabled');
    (usable ? target : document.querySelector('.tab.on'))?.focus();
  }
}
