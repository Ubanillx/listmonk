// HTML tooltips must escape chart containment and table/modal overflow.
export function chartTooltip(element, option = {}) {
  return {
    ...option,
    renderMode: 'html',
    appendTo: () => document.body,
    confine: false,
    className: 'lm-chart-tooltip',
    extraCssText: 'z-index: 1000; max-width: min(420px, calc(100vw - 16px)); box-sizing: border-box; white-space: normal; overflow-wrap: anywhere;',
    position: (point, params, content, target, size) => {
      const rect = element.getBoundingClientRect();
      const scaleX = rect.width / element.clientWidth || 1;
      const scaleY = rect.height / element.clientHeight || 1;
      const x = rect.left + point[0] * scaleX;
      const y = rect.top + point[1] * scaleY;
      const [width, height] = size.contentSize;
      let left = x + 16;
      let top = y + 16;
      if (left + width > window.innerWidth - 8) left = x - width - 16;
      if (top + height > window.innerHeight - 8) top = y - height - 16;
      left = Math.max(8, Math.min(left, window.innerWidth - width - 8));
      top = Math.max(8, Math.min(top, window.innerHeight - height - 8));
      return [(left - rect.left) / scaleX, (top - rect.top) / scaleY];
    },
  };
}

// A portalled tooltip must not remain behind when its trigger scrolls away.
export function dismissChartTooltipOnMove(chart) {
  const hide = () => chart.dispatchAction({ type: 'hideTip' });
  window.addEventListener('scroll', hide, true);
  window.addEventListener('resize', hide);
  return () => {
    window.removeEventListener('scroll', hide, true);
    window.removeEventListener('resize', hide);
  };
}
