<template>
  <!--
    A viewport-aware replacement for b-tooltip on the failure actions. Buefy
    positions its bubble relative to the trigger, so inside the table's
    horizontal scroll container a long label is clipped and never flips at the
    window edge. This bubble is fixed-positioned, measures itself, flips above
    or below the trigger and clamps horizontally inside the viewport.
  -->
  <span class="icon-tip" @mouseenter="show" @mouseleave="hide" @focusin="show" @focusout="hide">
    <button ref="trigger" type="button" class="provider-icon-button" :class="{ 'is-blocked': blocked }"
      :disabled="disabled" :aria-label="label" :data-cy="dataCy" @click="$emit('click')">
      <b-icon :icon="icon" size="is-small" />
    </button>
    <span v-if="visible" ref="bubble" class="icon-tip-bubble" role="tooltip" :style="bubbleStyle">{{ label }}</span>
  </span>
</template>

<script>
const EDGE = 8;

export default {
  name: 'IconTip',
  props: {
    label: { type: String, required: true },
    icon: { type: String, required: true },
    disabled: { type: Boolean, default: false },
    blocked: { type: Boolean, default: false },
    dataCy: { type: String, default: '' },
  },
  data() {
    return { visible: false, bubbleStyle: {} };
  },
  mounted() {
    // A fixed bubble would otherwise stay behind while the page or table
    // scrolls, so close it instead of tracking every scroll container.
    window.addEventListener('scroll', this.hide, true);
    window.addEventListener('resize', this.hide);
  },
  beforeDestroy() {
    window.removeEventListener('scroll', this.hide, true);
    window.removeEventListener('resize', this.hide);
  },
  methods: {
    show() {
      this.visible = true;
      this.$nextTick(this.position);
    },
    hide() {
      this.visible = false;
    },
    position() {
      const { trigger, bubble } = this.$refs;
      if (!trigger || !bubble) return;
      const rect = trigger.getBoundingClientRect();
      const size = bubble.getBoundingClientRect();
      let top = rect.top - size.height - EDGE;
      // Flip below when the bubble would leave the top of the viewport.
      if (top < EDGE) top = rect.bottom + EDGE;
      // A trigger near the bottom edge must still keep the whole bubble visible.
      top = Math.max(EDGE, Math.min(top, window.innerHeight - size.height - EDGE));
      let left = rect.left + rect.width / 2 - size.width / 2;
      // Clamp so a bubble near either window edge stays fully visible.
      left = Math.max(EDGE, Math.min(left, window.innerWidth - size.width - EDGE));
      this.bubbleStyle = { top: `${top}px`, left: `${left}px` };
    },
  },
};
</script>

<style lang="scss" scoped>
.icon-tip { display: inline-flex; }

.provider-icon-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  border: 0;
  border-radius: var(--lm-radius-sm);
  background: none;
  color: var(--lm-color-primary);
  cursor: pointer;

  &:hover { background: var(--lm-color-surface-subtle); }
  &:focus-visible { outline: 2px solid var(--lm-color-primary); outline-offset: 2px; }
  &:disabled { color: var(--lm-color-text-muted); cursor: not-allowed; background: none; }
  &.is-blocked { color: var(--lm-color-danger); }

  .icon { pointer-events: none; }
}

.icon-tip-bubble {
  position: fixed;
  z-index: 1000;
  width: max-content;
  max-width: min(280px, calc(100vw - 16px));
  padding: 6px 10px;
  border-radius: var(--lm-radius-sm);
  background: rgba(16, 24, 40, 0.92);
  color: #fff;
  font-size: 12px;
  line-height: 1.4;
  white-space: normal;
  overflow-wrap: anywhere;
  box-shadow: var(--lm-shadow-sm);
  pointer-events: none;
}
</style>
