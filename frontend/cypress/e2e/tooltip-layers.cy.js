/* eslint-env mocha */
/* global cy, expect */
/* eslint-disable no-underscore-dangle -- Inspect mounted Vue components in the browser. */

const visibleTooltip = '.lm-chart-tooltip';
// Let native scroll/focus and ResizeObserver notifications complete before hover.
const settleLayout = () => cy.window().then((win) => new Promise((resolve) => {
  win.requestAnimationFrame(() => win.requestAnimationFrame(resolve));
}));
const shown = (element) => {
  const style = element.ownerDocument.defaultView.getComputedStyle(element);
  return element.offsetHeight > 0 && style.visibility !== 'hidden' && style.display !== 'none';
};
const withinViewport = (element, win) => {
  const rect = element.getBoundingClientRect();
  expect(rect.width).to.be.greaterThan(0);
  expect(rect.left).to.be.at.least(7);
  expect(rect.top).to.be.at.least(7);
  expect(rect.right).to.be.at.most(win.innerWidth - 7);
  expect(rect.bottom).to.be.at.most(win.innerHeight - 7);
};

describe('Tooltip layers', () => {
  before(() => {
    cy.resetDB();
    cy.loginAndVisit('/admin/');
  });

  [1685, 390].forEach((width) => {
    it(`keeps long delivery tooltips outside chart containment at ${width}px`, () => {
      cy.viewport(width, 1244);
      cy.get('.dashboard-delivery-chart .dashboard-chart').scrollIntoView().then(($el) => {
        const vm = $el[0].__vue__;
        const option = vm.option;
        option.animation = false;
        option.yAxis.data = [`#115 · ${'长活动名称 Long campaign '.repeat(12)}`];
        option.series.forEach((series, i) => { series.data = [114 + i]; });
        vm.chart.setOption({ ...option, tooltip: vm.chart.getOption().tooltip[0] }, true);
        vm.chart.dispatchAction({ type: 'showTip', seriesIndex: 0, dataIndex: 0 });
      });
      cy.get(visibleTooltip).should(($tips) => {
        const tips = [...$tips].filter(shown);
        expect(tips).to.have.length(1);
        const [tip] = tips;
        expect(tip.textContent).to.contain('#115');
        const win = tip.ownerDocument.defaultView;
        expect(tip.parentElement).to.eq(win.document.body);
        withinViewport(tip, win);
      });
      cy.window().then((win) => { win.dispatchEvent(new win.Event('scroll')); });
      cy.get(visibleTooltip).should(($tips) => { expect([...$tips].filter(shown)).to.have.length(0); });
    });
  });

  it('portals button hints above clipping and modal layers, and cleans up', () => {
    cy.viewport(390, 844);
    cy.window().then((win) => {
      const Vue = win.document.querySelector('#app').__vue__.$options._base;
      const host = win.document.createElement('div');
      win.document.body.appendChild(host);
      const fixture = new Vue({
        render(h) {
          return h('div', { style: { position: 'fixed', right: '0', top: '0', width: '120px', overflow: 'hidden', contain: 'paint', zIndex: 960 } }, [
            h('b-tooltip', { props: { label: '长提示 tooltip '.repeat(24), type: 'is-dark' } }, [
              h('button', { attrs: { 'data-cy': 'clipped-hint' } }, '提示'),
            ]),
          ]);
        },
      }).$mount(host);
      expect(fixture.$children[0].appendToBody).to.eq(true);
      expect(fixture.$children[0].triggers).to.include('focus');
      cy.wrap(fixture).as('fixture');
    });
    settleLayout();
    cy.get('[data-cy=clipped-hint]').then(($button) => { $button[0].focus(); });
    cy.get('@fixture').its('$children.0.isActive').should('eq', true);
    cy.get('.lm-tooltip-portal .tooltip-content').should(($tips) => {
      const [tip] = [...$tips].filter((el) => shown(el) && el.textContent.includes('长提示'));
      expect(tip, 'visible hint').to.exist;
      const win = tip.ownerDocument.defaultView;
      withinViewport(tip, win);
      expect(tip.closest('.lm-tooltip-portal').parentElement).to.eq(win.document.body);
    });
    cy.get('[data-cy=clipped-hint]').trigger('keydown', { key: 'Escape' });
    cy.document().trigger('keyup', { key: 'Escape' });
    cy.get('.lm-tooltip-portal .tooltip-content').should(($tips) => { expect([...$tips].filter(shown)).to.have.length(0); });
    cy.get('@fixture').then((fixture) => {
      fixture.$destroy();
      fixture.$el.remove();
    });
    cy.get('.lm-tooltip-portal .tooltip-content').should('not.contain', '长提示');
  });

  it('keeps legacy canvas tooltips readable and removes them on navigation', () => {
    cy.intercept('GET', '/api/campaigns/report/summary*', {
      body: { data: { sent: 114, bounced: 0, unique_viewers: 114, unique_clickers: 114 } },
    });
    cy.visit('/admin/campaigns/analytics');
    cy.viewport(390, 844);
    cy.get('.breakdown-chart .chart').scrollIntoView();
    settleLayout();
    cy.get('.breakdown-chart .chart').then(($el) => {
      const { chart } = $el[0].__vue__;
      chart.data.labels = ['长活动名称 Long campaign '.repeat(12)];
      chart.data.datasets[0].data = [114];
      chart.update('none');
      chart.tooltip.setActiveElements([{ datasetIndex: 0, index: 0 }], { x: 100, y: 50 });
      chart.update('none');
    });
    cy.get('.lm-canvas-tooltip').should('contain', '100.00%').should(($tip) => {
      expect(shown($tip[0]), 'visible canvas hint').to.eq(true);
      const win = $tip[0].ownerDocument.defaultView;
      expect($tip[0].parentElement).to.eq(win.document.body);
      withinViewport($tip[0], win);
    });
    cy.visit('/admin/');
    cy.get('.lm-canvas-tooltip').should('not.exist');
  });
});
