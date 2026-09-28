/*
 * Accessibility shim for the Vue 2 + Buefy admin UI.
 *
 * Two gaps cannot be fixed from the templates without editing hundreds of
 * fields or forking Buefy:
 *
 * 1. `b-field`'s documentation requires an explicit `label-for` on the field
 *    and a matching `id` on the control. Without them Buefy renders a plain
 *    `<label class="label">` that is not programmatically associated with the
 *    control, so assistive technology announces every input without a name.
 * 2. `b-pagination` renders its previous/next controls as icon-only anchors
 *    with no accessible name at all.
 *
 * This shim closes both once, at runtime: after a field component mounts, each
 * control inside it that has no accessible name of its own inherits the field's
 * visible label (or the placeholder, for search boxes) as `aria-label`.
 * Explicit markup always wins: a control with `aria-label`, `aria-labelledby`,
 * an `id` referenced by a `label[for]`, or a wrapping `<label>` is left
 * untouched.
 */

const labelText = (el) => (
  el && el.textContent ? el.textContent.replace(/\s+/g, ' ').trim() : ''
);

const controlSelector = 'input:not([type=hidden]):not([type=submit]):not([type=button]), select, textarea';

// A control is already named when any of the standard associations is present.
const hasAccessibleName = (control) => {
  if (control.getAttribute('aria-label') || control.getAttribute('aria-labelledby')) {
    return true;
  }
  if (control.closest('label')) {
    return true;
  }
  const id = control.getAttribute('id');
  if (id && typeof CSS !== 'undefined' && CSS.escape) {
    try {
      if (document.querySelector(`label[for="${CSS.escape(id)}"]`)) {
        return true;
      }
    } catch (e) { /* malformed id: treat as unlabelled */ }
  }

  return false;
};

const labelFields = (vm) => {
  const root = vm.$el;
  if (!root || root.nodeType !== 1 || !root.classList || !root.classList.contains('field')) {
    return;
  }

  // Buefy renders the visible label as `.field-label > label.label` (or a bare
  // `.label` for `label-position` variants).
  const label = root.querySelector(':scope > .field-label > label, :scope > label.label');
  const text = labelText(label);

  root.querySelectorAll(controlSelector).forEach((control) => {
    if (hasAccessibleName(control)) {
      return;
    }

    // Fall back to the placeholder for controls whose meaning lives in the
    // placeholder (search boxes, inline filters) rather than in a field label.
    const name = text || (control.getAttribute('placeholder') || '').trim();
    if (name) {
      control.setAttribute('aria-label', name);
    }
  });
};

const labelPagination = (vm, i18n) => {
  if (!vm.$el || vm.$el.nodeType !== 1 || !vm.$el.querySelectorAll) {
    return;
  }

  const previous = vm.$el.querySelector('.pagination-previous');
  const next = vm.$el.querySelector('.pagination-next');
  if (previous && !previous.getAttribute('aria-label')) {
    previous.setAttribute('aria-label', i18n.t('globals.buttons.previousPage'));
  }
  if (next && !next.getAttribute('aria-label')) {
    next.setAttribute('aria-label', i18n.t('globals.buttons.nextPage'));
  }
};

export function installA11y(Vue, i18n) {
  const apply = (vm) => {
    if (vm.$options.name === 'BPagination') {
      labelPagination(vm, i18n);
      return;
    }

    labelFields(vm);
  };

  Vue.mixin({
    mounted() {
      // Wait for the render to settle so nested controls exist.
      this.$nextTick(() => apply(this));
    },

    // Labels can change with the active locale or with the validation state.
    updated() {
      apply(this);
    },
  });
}

export default installA11y;
