/* eslint-env mocha */
/* global cy, expect */

describe('Dashboard customer groups', () => {
  it('renders private and public-pool statistics in separate cards', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/dashboard/counts').as('dashboardCounts');
    cy.loginAndVisit('/admin/');

    cy.wait('@dashboardCounts').its('response.body.data').then((counts) => {
      expect(counts.private_customers.total).to.eq(2);
      expect(counts.pool_customers).to.include({ total: 0, active: 0, removed: 0 });
      expect(counts.pool_lists).to.deep.eq({ total: 0, bound: 0, unbound: 0, allocations: 0, organizations: 0 });
    });
    cy.get('[data-cy=private-customers] .metric-value').should('contain', '2');
    cy.get('[data-cy=dashboard-pool-customers] .metric-value').should('contain', '0');
    cy.get('[data-cy=pool-lists] .metric-value').should('exist');
    cy.get('[data-cy=messages] .metric-value').should('contain', '0');
  });

  it('shows pool bindings, allocations and distinct organizations from live data', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/');
    const organizations = [];
    const pools = [];
    cy.request('/api/profile').then(({ body }) => {
      ['North', 'South'].forEach((name) => {
        cy.request('POST', '/api/organizations', { name, manager_user_id: body.data.id })
          .then(({ body: result }) => { organizations.push(result.data.id); });
      });
    });
    ['Shared pool', 'North pool', 'Unbound pool'].forEach((name) => {
      cy.request('POST', '/api/customer-lists', { name, type: 'pool', optin: 'single' })
        .then(({ body }) => { pools.push(body.data.id); });
    });
    [[0, 0], [0, 1], [1, 0]].forEach(([pool, organization]) => {
      cy.then(() => cy.request('POST', '/api/org-pool-allocations', {
        pool_id: pools[pool], organization_id: organizations[organization], name: `Allocation ${pool}/${organization}`,
      }));
    });
    cy.intercept('GET', '/api/dashboard/counts').as('populatedCounts');
    cy.get('[data-cy=btn-refresh]').click();
    cy.wait('@populatedCounts').its('response.body.data.pool_lists')
      .should('deep.eq', { total: 3, bound: 2, unbound: 1, allocations: 3, organizations: 2 });
    cy.get('[data-cy=pool-lists] .metric-value').should('contain', '3');
    cy.get('[data-cy=pool-lists-bound]').should('have.text', '2');
    cy.get('[data-cy=pool-lists-unbound]').should('have.text', '1');
    cy.get('[data-cy=pool-lists-allocations]').should('have.text', '3');
    cy.get('[data-cy=pool-lists-organizations]').should('have.text', '2');
    cy.get('[data-cy=pool-lists] .metric-breakdown li').should('have.length', 4);
    cy.viewport(390, 844);
    cy.get('[data-cy=pool-lists-bound]').should('be.visible');
    cy.document().then((doc) => { expect(doc.documentElement.scrollWidth).to.be.at.most(390); });
  });
});
