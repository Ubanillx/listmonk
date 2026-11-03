// Chart.js' built-in canvas tooltip cannot escape the canvas bounds. Keep its
// formatted title/body (including callbacks), rendering text in a body portal.
export default function canvasTooltip() {
  let bubble;
  const hide = () => { if (bubble) bubble.hidden = true; };
  const text = (parent, value, className) => {
    const row = document.createElement('div');
    row.className = className;
    row.textContent = value;
    parent.appendChild(row);
    return row;
  };
  return {
    id: 'listmonkTooltip',
    afterInit() {
      window.addEventListener('scroll', hide, true);
      window.addEventListener('resize', hide);
    },
    afterDestroy() {
      window.removeEventListener('scroll', hide, true);
      window.removeEventListener('resize', hide);
      if (bubble) bubble.remove();
    },
    external({ chart, tooltip }) {
      if (!tooltip.opacity) {
        hide();
        return;
      }
      if (!bubble) {
        bubble = document.createElement('div');
        bubble.className = 'lm-canvas-tooltip';
        bubble.setAttribute('role', 'tooltip');
        document.body.appendChild(bubble);
      }
      bubble.replaceChildren();
      (tooltip.title || []).forEach((line) => text(bubble, line, 'lm-canvas-tooltip-title'));
      (tooltip.beforeBody || []).forEach((line) => text(bubble, line, ''));
      (tooltip.body || []).forEach((body, index) => {
        const row = text(bubble, [...body.before, ...body.lines, ...body.after].join('\n'), 'lm-canvas-tooltip-row');
        const color = tooltip.labelColors[index];
        if (tooltip.options.displayColors && color) {
          const marker = document.createElement('span');
          marker.className = 'lm-canvas-tooltip-marker';
          marker.style.backgroundColor = color.backgroundColor;
          row.prepend(marker);
        }
      });
      [...(tooltip.afterBody || []), ...(tooltip.footer || [])].forEach((line) => text(bubble, line, ''));
      bubble.hidden = false;
      const rect = chart.canvas.getBoundingClientRect();
      const x = rect.left + tooltip.caretX * (rect.width / chart.width);
      const y = rect.top + tooltip.caretY * (rect.height / chart.height);
      const size = bubble.getBoundingClientRect();
      let left = x + 16;
      let top = y + 16;
      if (left + size.width > window.innerWidth - 8) left = x - size.width - 16;
      if (top + size.height > window.innerHeight - 8) top = y - size.height - 16;
      bubble.style.left = `${Math.max(8, Math.min(left, window.innerWidth - size.width - 8))}px`;
      bubble.style.top = `${Math.max(8, Math.min(top, window.innerHeight - size.height - 8))}px`;
    },
  };
}
