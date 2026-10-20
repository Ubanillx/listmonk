// Insert through the browser's editing command so text fields retain native undo.
// The fallback also notifies Vue/React through their normal input event path.
export default function insertInputText(input, text) {
  if (!input?.isConnected || input.disabled || input.readOnly || input.selectionStart === null
    || typeof input.selectionStart !== 'number') return false;

  const start = input.selectionStart;
  const end = input.selectionEnd;
  const doc = input.ownerDocument;
  input.focus();
  input.setSelectionRange(start, end);
  if (!doc.execCommand('insertText', false, text)) {
    const value = input.value.slice(0, start) + text + input.value.slice(end);
    const win = doc.defaultView;
    const prototype = input.tagName === 'TEXTAREA' ? win.HTMLTextAreaElement.prototype : win.HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, 'value').set.call(input, value);
    input.setSelectionRange(start + text.length, start + text.length);
    input.dispatchEvent(new win.Event('input', { bubbles: true }));
  }
  return true;
}
