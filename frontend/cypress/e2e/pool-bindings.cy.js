/* eslint-env mocha */
/* global cy, expect */

describe('Organization pool bindings', () => {
  it('compares organizations, creates a binding and preserves the workspace and drafts', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/customer-lists');
    cy.get('[data-cy=btn-new]').should('be.visible');
    const organizations = {};
    let poolID;
    let workspaceID;
    cy.window().then((win) => { workspaceID = win.localStorage.getItem('listmonk.workspace.organizationId'); });
    cy.request('/api/profile').then(({ body }) => {
      ['North', 'South', 'West'].forEach((name) => {
        cy.request('POST', '/api/organizations', { name, manager_user_id: body.data.id })
          .then(({ body: result }) => { organizations[name] = result.data.id; });
      });
    });
    cy.request('POST', '/api/customer-lists', { name: 'Binding comparison pool', type: 'pool', optin: 'single' })
      .then(({ body }) => { poolID = body.data.id; });
    cy.then(() => cy.request('POST', '/api/org-pool-allocations', {
      pool_id: poolID, organization_id: organizations.North, name: 'North allocation',
    }));
    cy.then(() => {
      cy.intercept({ method: 'GET', url: `/api/customer-lists/${poolID}/org-pool-allocations`, times: 1 },
        { statusCode: 500, body: { message: 'Temporary fixture failure' } });
    });
    cy.visit('/admin/customers/import');
    cy.get('[data-cy=import-tab-pool]').click();
    cy.get('.customer_list-selector input').type('Binding comparison pool');
    cy.contains('.customer_list-selector .autocomplete a', 'Binding comparison pool').click();
    cy.get('[data-cy=pool-bindings-retry]').should('be.visible');
    cy.get('[data-cy=create-org-pool-allocation]').should('not.exist');
    cy.get('[data-cy=pool-bindings-retry]').click();
    cy.contains('[data-cy=pool-organization-option]', 'North').find('.pool-bindings__status').should('contain', 'Bound').and('not.contain', 'Unbound');
    cy.contains('[data-cy=pool-organization-option]', 'South').find('.pool-bindings__status').should('contain', 'Unbound');
    cy.contains('[data-cy=pool-organization-option]', 'North').click();
    cy.get('[data-cy=org-pool-allocation-summary]').should('contain', 'North allocation');
    cy.contains('[data-cy=pool-organization-option]', 'South').click();
    cy.get('[data-cy=pool-current-organization]').should('have.text', 'South');
    cy.get('[data-cy=org-pool-allocation-name]').should('have.value', 'South pool allocation').clear().type('South sales allocation');
    cy.contains('[data-cy=pool-organization-option]', 'North').click();
    cy.contains('[data-cy=pool-organization-option]', 'South').click();
    cy.get('[data-cy=org-pool-allocation-name]').should('have.value', 'South sales allocation');
    cy.intercept('POST', '/api/org-pool-allocations').as('bindOrganization');
    cy.get('[data-cy=create-org-pool-allocation]').click();
    cy.wait('@bindOrganization').then(({ request, response }) => {
      expect(response.statusCode).to.eq(200);
      expect(request.body.organization_id).to.eq(organizations.South);
      expect(request.body.pool_id).to.eq(poolID);
      expect(request.body.name).to.eq('South sales allocation');
    });
    cy.get('[data-cy=org-pool-allocation-summary]').should('contain', 'South sales allocation');
    cy.contains('[data-cy=pool-organization-option]', 'South').find('.pool-bindings__status').should('contain', 'Bound').and('not.contain', 'Unbound');
    cy.get('[data-cy=create-org-pool-allocation]').should('not.exist');
    cy.window().then((win) => { expect(win.localStorage.getItem('listmonk.workspace.organizationId')).to.eq(workspaceID); });
    cy.get('[data-cy=pool-organization-search]').type('West');
    cy.get('[data-cy=pool-organization-option]').should('have.length', 1).click();
    cy.get('[data-cy=pool-current-organization]').should('have.text', 'West');
    cy.get('[data-cy=pool-organization-search]').clear();
    cy.viewport(1440, 1000);
    cy.get('.pool-bindings__organizations').then(($left) => {
      cy.get('.pool-bindings__detail').then(($right) => {
        expect($right[0].getBoundingClientRect().left).to.be.greaterThan($left[0].getBoundingClientRect().left);
      });
    });
    cy.viewport(390, 844);
    cy.get('.pool-bindings__organizations').then(($top) => {
      cy.get('.pool-bindings__detail').then(($bottom) => {
        expect($bottom[0].getBoundingClientRect().top).to.be.at.least($top[0].getBoundingClientRect().bottom);
      });
    });
    cy.document().then((doc) => { expect(doc.documentElement.scrollWidth).to.be.at.most(390); });
  });
});
