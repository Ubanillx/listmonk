/* eslint-env mocha */
/* global cy, expect */

const pager = () => cy.get('[data-cy=pagination]');
const sizes = () => cy.get('select[data-cy=page-size]');
const expectConsistentControls = () => {
  pager().find('select, input, button, .pagination-link').should(($controls) => {
    [...$controls].forEach((control) => {
      const style = control.ownerDocument.defaultView.getComputedStyle(control);
      expect(style.fontSize, control.outerHTML).to.equal('13px');
      expect(style.borderRadius, control.outerHTML).to.equal('6px');
      expect(control.getBoundingClientRect().height, control.outerHTML).to.equal(32);
    });
  });
};

describe('Shared pagination', () => {
  before(() => { cy.resetDB(); });
  beforeEach(() => {
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
  });

  it('loads the table adapter, jumps pages and synchronizes page sizes without losing filters', () => {
    const rows = Array.from({ length: 65 }, (_, index) => ({
      id: index + 1,
      customer_code: `PAGE-${index + 1}`,
      email: `page${index + 1}@example.test`,
      name: `Pagination ${index + 1}`,
      status: 'enabled',
      attribs: {},
      customer_lists: [],
      owner_user_id: 1,
      created_at: '2026-10-19T00:00:00Z',
      updated_at: '2026-10-19T00:00:00Z',
    }));
    cy.intercept({ method: 'GET', pathname: '/api/customers' }, (req) => {
      const size = Number(req.query.per_page || 20);
      const page = Number(req.query.page || 1);
      req.reply({
        data: {
          results: rows.slice((page - 1) * size, page * size), total: rows.length, page, per_page: size,
        },
      });
    }).as('customers');
    cy.loginAndVisit('/admin/customers');
    cy.wait('@customers');
    pager().should('have.length', 2);
    expectConsistentControls();
    cy.viewport(1280, 950);
    cy.get('.app-table-pagination').first().should(($bar) => {
      const left = $bar[0].querySelector('.level-left').getBoundingClientRect();
      const right = $bar[0].querySelector('.level-right').getBoundingClientRect();
      expect(Math.abs((left.top + left.height / 2) - (right.top + right.height / 2))).to.be.lessThan(1);
    });
    cy.get('[data-cy=search]').type('Pagination');
    cy.get('[data-cy=btn-search]').click();
    cy.wait('@customers');
    cy.get('tbody tr').should('have.length', 20);
    cy.get('[data-cy=page-jump]').first().clear().type('3{enter}');
    cy.wait('@customers').its('request.query').should('include', { page: '3', search: 'pagination' });
    cy.get('tbody tr').first().should('contain', 'PAGE-41');
    cy.get('[data-cy=page-jump]').last().should('have.value', '3');
    sizes().last().select('50');
    cy.wait('@customers').its('request.query').should('include', { page: '1', per_page: '50', search: 'pagination' });
    sizes().should(($selects) => { [...$selects].forEach((select) => expect(select.value).to.equal('50')); });
    cy.get('tbody tr').should('have.length', 50);
    cy.get('[data-cy=page-jump]').first().clear().type('2');
    cy.get('[data-cy=page-jump-submit]').first().click();
    cy.wait('@customers').its('request.query').should('include', { page: '2', per_page: '50' });
    cy.get('tbody tr').should('have.length', 15).first().should('contain', 'PAGE-51');
    sizes().first().select('100');
    cy.wait('@customers');
    sizes().first().select('10');
    cy.wait('@customers');
    cy.get('tbody tr').should('have.length', 10);
    cy.viewport(390, 844);
    pager().first().should('be.visible');
    expectConsistentControls();
    pager().each(($pager) => {
      const bounds = $pager[0].getBoundingClientRect();
      expect(bounds.left).to.be.at.least(0);
      expect(bounds.right).to.be.at.most(390);
      [...$pager[0].querySelectorAll('select, input, button')].forEach((control) => {
        expect(control.getBoundingClientRect().right).to.be.at.most(390);
      });
    });
    cy.document().then((doc) => { expect(doc.documentElement.scrollWidth).to.be.at.most(390); });
    cy.viewport(1440, 950);
    expectConsistentControls();
    cy.document().then((doc) => { expect(doc.documentElement.scrollWidth).to.be.at.most(1440); });
    cy.screenshot('pagination-desktop', { capture: 'viewport' });
  });

  it('keeps standalone audit pagination available for single-page results', () => {
    cy.intercept({ method: 'GET', pathname: '/api/audit-events' }, (req) => {
      req.reply({
        data: {
          results: [], total: 0, page: Number(req.query.page), per_page: Number(req.query.per_page),
        },
      });
    }).as('audit');
    cy.loginAndVisit('/admin/settings/audit');
    cy.wait('@audit');
    pager().should('have.length', 1);
    sizes().select('10');
    cy.wait('@audit').its('request.query').should('include', { page: '1', per_page: '10' });
    cy.get('[data-cy=page-jump]').should('have.attr', 'max', '1');
  });

  it('serves backend appearance assets without a doubled admin prefix in production and Vite', () => {
    cy.loginAndVisit('/admin/');
    cy.document().then((doc) => {
      expect(doc.querySelector('script[src*="custom.js"]').getAttribute('src')).to.match(/^\/admin\/custom\.js\?/);
      expect(doc.querySelector('link[href*="custom.css"]').getAttribute('href')).to.match(/^\/admin\/custom\.css\?/);
    });
    cy.request('/admin/custom.js').its('status').should('equal', 200);
    cy.request('/admin/custom.css').its('status').should('equal', 200);
    cy.request('http://127.0.0.1:8181/admin/').its('body').then((html) => {
      expect(html).to.contain('src="/admin/custom.js?v=asset_version"');
      expect(html).to.contain('href="/admin/custom.css?v=asset_version"');
      expect(html).not.to.contain('/admin/admin/');
    });
    cy.request('http://127.0.0.1:8181/admin/src/components/PaginatedTable.js').its('body')
      .should('contain', 'buefy_dist_esm_table');
  });
});
