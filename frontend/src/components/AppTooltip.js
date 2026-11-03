/* eslint-disable no-underscore-dangle -- Buefy's portal and Vue's lifecycle flags. */
// Keep Buefy's slots, triggers and appearance, but share one safe portal policy.
export default function installTooltips(Vue) {
  const Tooltip = Vue.component('BTooltip');
  Vue.component('BTooltip', {
    extends: Tooltip,
    props: {
      appendToBody: { type: Boolean, default: true },
      triggers: { type: Array, default: () => ['hover', 'focus'] },
    },
    mounted() {
      window.addEventListener('scroll', this.moveTooltip, true);
      window.addEventListener('resize', this.moveTooltip);
    },
    updated() {
      this.updateAppendToBody();
    },
    beforeDestroy() {
      window.removeEventListener('scroll', this.moveTooltip, true);
      window.removeEventListener('resize', this.moveTooltip);
    },
    methods: {
      moveTooltip() {
        const { trigger } = this.$refs;
        // Focusing a control can scroll it into view after the focus event.
        // Keep keyboard hints attached; pointer-only hints close on movement.
        if (trigger && trigger.contains(document.activeElement) && this.isActive) {
          const rect = trigger.getBoundingClientRect();
          if (rect.top >= 0 && rect.bottom <= window.innerHeight && rect.left >= 0 && rect.right <= window.innerWidth) {
            this.updateAppendToBody();
            return;
          }
        }
        this.dismissTooltip();
      },
      dismissTooltip() {
        clearTimeout(this.timer);
        this.isActive = false;
      },
      keyPress({ key }) {
        if (key === 'Escape' || key === 'Esc') this.dismissTooltip();
      },
      updateAppendToBody() {
        this.$nextTick(() => {
          const wrapper = this.$data._bodyEl;
          const { content, trigger } = this.$refs;
          if (!this.appendToBody || !wrapper || !content || !trigger || this._isDestroyed) return;
          const root = wrapper.children[0];
          const rect = trigger.getBoundingClientRect();
          wrapper.classList.add('lm-tooltip-portal');
          Object.assign(wrapper.style, {
            position: 'fixed', top: '0', left: '0', width: '0', zIndex: '1000',
          });
          root.className = this.$el.className;
          Object.assign(root.style, {
            position: 'fixed',
            top: `${rect.top}px`,
            left: `${rect.left}px`,
            width: `${rect.width}px`,
            height: `${rect.height}px`,
          });
          content.setAttribute('role', 'tooltip');
          if (!this.active || !(this.isActive || this.always)) return;
          const opposite = {
            'is-top': 'is-bottom', 'is-bottom': 'is-top', 'is-left': 'is-right', 'is-right': 'is-left',
          };
          let box = content.getBoundingClientRect();
          if ((this.position === 'is-top' && box.top < 8)
            || (this.position === 'is-bottom' && box.bottom > window.innerHeight - 8)
            || (this.position === 'is-left' && box.left < 8)
            || (this.position === 'is-right' && box.right > window.innerWidth - 8)) {
            root.classList.replace(this.position, opposite[this.position]);
            box = content.getBoundingClientRect();
          }
          const dx = Math.max(8, Math.min(box.left, window.innerWidth - box.width - 8)) - box.left;
          const dy = Math.max(8, Math.min(box.top, window.innerHeight - box.height - 8)) - box.top;
          root.style.left = `${rect.left + dx}px`;
          root.style.top = `${rect.top + dy}px`;
        });
      },
    },
  });
}
