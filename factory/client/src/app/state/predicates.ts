// The four predicates a run on a candidate environment decides, decided here
// over the rendered DOM of each state a screen declares. The factory holds its
// own screens to what it holds a product's screens to, so these are the same
// four and not a weaker set: a contrast floor, a name on every control, a
// focus order, and a target size.
//
// Each returns the violations it found. An empty array is a pass, and a
// state a screen's spec never rendered leaves every predicate over that state
// undecided — which is why each screen's spec enumerates its declared states
// rather than opening whichever one is convenient.
//
// What defines them:
// ../../../../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/01-the-third-outcome.md.

// The floor is the ratio the regulation states for body text.
export const CONTRAST_FLOOR = 4.5;

// The smallest side of a control, in CSS pixels.
export const TARGET_SIZE = 24;

const CONTROLS = 'a[href], button, input, select, textarea, [tabindex]';

export function contrastViolations(root: Element, floor = CONTRAST_FLOOR): string[] {
  const found: string[] = [];
  const walker = root.ownerDocument.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node !== null; node = walker.nextNode()) {
    const text = node.textContent ?? '';
    if (text.trim() === '') {
      continue;
    }
    const element = node.parentElement;
    if (element === null || !rendered(element)) {
      continue;
    }
    const foreground = parseColour(getComputedStyle(element).color);
    const background = backgroundOf(element);
    if (foreground === undefined || background === undefined) {
      found.push(`no resolvable colour behind "${excerpt(text)}"`);
      continue;
    }
    const ratio = contrast(foreground, background);
    if (ratio < floor) {
      found.push(`"${excerpt(text)}" is ${ratio.toFixed(2)}:1 against its background`);
    }
  }
  return found;
}

export function unnamedControls(root: Element): string[] {
  const found: string[] = [];
  for (const control of controlsOf(root)) {
    if (accessibleName(control) === '') {
      found.push(`${describe(control)} has no accessible name`);
    }
  }
  return found;
}

// Tabbable elements are reached in document order, which holds when no
// element raises itself with a positive tabindex and every one of them is
// actually focusable.
export function focusOrderViolations(root: Element): string[] {
  const found: string[] = [];
  for (const control of controlsOf(root)) {
    const declared = control.getAttribute('tabindex');
    if (declared !== null && Number(declared) > 0) {
      found.push(`${describe(control)} raises itself with tabindex ${declared}`);
      continue;
    }
    control.focus();
    if (root.ownerDocument.activeElement !== control) {
      found.push(`${describe(control)} is in the tab sequence and cannot take focus`);
    }
  }
  return found;
}

export function targetSizeViolations(root: Element, min = TARGET_SIZE): string[] {
  const found: string[] = [];
  for (const control of controlsOf(root)) {
    const box = control.getBoundingClientRect();
    if (box.width < min || box.height < min) {
      found.push(
        `${describe(control)} is ${box.width.toFixed(1)}x${box.height.toFixed(1)}, under ${min}`,
      );
    }
  }
  return found;
}

// Every predicate over one rendered state, as the one call a spec makes per
// state it drives the screen into.
export function predicateViolations(root: Element): string[] {
  return [
    ...contrastViolations(root),
    ...unnamedControls(root),
    ...focusOrderViolations(root),
    ...targetSizeViolations(root),
  ];
}

function controlsOf(root: Element): HTMLElement[] {
  const all = [...root.querySelectorAll<HTMLElement>(CONTROLS)];
  return all.filter((element) => {
    if (!rendered(element)) {
      return false;
    }
    if (element.hasAttribute('disabled')) {
      return false;
    }
    const declared = element.getAttribute('tabindex');
    return declared === null || Number(declared) >= 0;
  });
}

function rendered(element: Element): boolean {
  const style = getComputedStyle(element);
  if (style.display === 'none' || style.visibility === 'hidden') {
    return false;
  }
  const box = element.getBoundingClientRect();
  return box.width > 0 || box.height > 0;
}

function accessibleName(control: HTMLElement): string {
  const label = control.getAttribute('aria-label');
  if (label !== null && label.trim() !== '') {
    return label.trim();
  }
  const labelledBy = control.getAttribute('aria-labelledby');
  if (labelledBy !== null) {
    const named = labelledBy
      .split(/\s+/)
      .map((id) => control.ownerDocument.getElementById(id)?.textContent ?? '')
      .join(' ')
      .trim();
    if (named !== '') {
      return named;
    }
  }
  if (
    control instanceof HTMLInputElement ||
    control instanceof HTMLSelectElement ||
    control instanceof HTMLTextAreaElement
  ) {
    for (const element of control.labels ?? []) {
      const named = element.textContent.trim();
      if (named !== '') {
        return named;
      }
    }
    if (
      control instanceof HTMLInputElement &&
      control.value.trim() !== '' &&
      (control.type === 'submit' || control.type === 'button')
    ) {
      return control.value.trim();
    }
  }
  const text = control.textContent.trim();
  if (text !== '') {
    return text;
  }
  return control.title.trim();
}

function describe(element: Element): string {
  const id = element.id === '' ? '' : `#${element.id}`;
  return `<${element.tagName.toLowerCase()}${id}>`;
}

function excerpt(text: string): string {
  const one = text.trim().replace(/\s+/g, ' ');
  return one.length > 40 ? `${one.slice(0, 40)}...` : one;
}

interface Colour {
  r: number;
  g: number;
  b: number;
  a: number;
}

// The first ancestor, this element included, whose own background is not
// transparent. A text node's contrast is decided against what is actually
// painted behind it and not against what its own rule declares.
function backgroundOf(element: Element): Colour | undefined {
  for (let at: Element | null = element; at !== null; at = at.parentElement) {
    const colour = parseColour(getComputedStyle(at).backgroundColor);
    if (colour !== undefined && colour.a > 0) {
      return colour;
    }
  }
  return undefined;
}

function parseColour(value: string): Colour | undefined {
  const parts = /^rgba?\(([^)]+)\)$/.exec(value.trim());
  if (parts === null) {
    return undefined;
  }
  const numbers = (parts[1] ?? '').split(/[\s,/]+/).filter((each) => each !== '');
  const [r, g, b, a] = numbers.map(Number);
  if (r === undefined || g === undefined || b === undefined) {
    return undefined;
  }
  return { r, g, b, a: a ?? 1 };
}

function contrast(foreground: Colour, background: Colour): number {
  const front = foreground.a >= 1 ? foreground : over(foreground, background);
  const one = luminance(front);
  const two = luminance(background);
  const lighter = Math.max(one, two);
  const darker = Math.min(one, two);
  return (lighter + 0.05) / (darker + 0.05);
}

function over(front: Colour, back: Colour): Colour {
  return {
    r: front.r * front.a + back.r * (1 - front.a),
    g: front.g * front.a + back.g * (1 - front.a),
    b: front.b * front.a + back.b * (1 - front.a),
    a: 1,
  };
}

function luminance(colour: Colour): number {
  const channel = (value: number): number => {
    const unit = value / 255;
    return unit <= 0.03928 ? unit / 12.92 : Math.pow((unit + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * channel(colour.r) + 0.7152 * channel(colour.g) + 0.0722 * channel(colour.b);
}
