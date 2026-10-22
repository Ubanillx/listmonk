/* eslint-env mocha */
/* global cy, Cypress, expect */

describe('Public pool filter layout', () => {
  it('keeps long pool names inside their filter beside the department filter', () => {
    expect(Cypress.config('baseUrl')).to.match(/^http:\/\/127\.0\.0\.1:92[78]3$/);
    cy.loginAndVisit('/admin/pool');
    cy.request('POST', '/api/customer-lists', {
      name: '户外销售三部公海 2026-10-19', type: 'pool', optin: 'single', tags: [],
    }).then(({ body }) => cy.request('POST', `/api/customer-lists/${body.data.id}/pool-contacts`, {
      customer_code: 'layout-1', name: 'Layout check', email: 'layout@example.com',
    }));
    cy.visit('/admin/pool');
    cy.get('[data-cy=pool-list-filter] option').should('have.length.greaterThan', 1);
    cy.get('[data-cy=pool-list-filter]').then(($select) => {
      const select = $select[0].getBoundingClientRect();
      const field = $select[0].closest('.pool-facet-filter').getBoundingClientRect();
      const department = Cypress.$('[data-cy=pool-department-filter]')[0].getBoundingClientRect();

      expect(select.right).to.be.at.most(field.right);
      expect(select.right).to.be.lessThan(department.left);
    });

    cy.viewport(760, 900);
    cy.get('[data-cy=pool-list-filter]').then(($select) => {
      const select = $select[0].getBoundingClientRect();
      const field = $select[0].closest('.pool-facet-filter').getBoundingClientRect();
      const department = Cypress.$('[data-cy=pool-department-filter]')[0].getBoundingClientRect();

      expect(select.right).to.be.at.most(field.right);
      expect(select.bottom).to.be.at.most(department.top);
    });
  });
});
