/* eslint-env mocha */
/* global cy */

describe('Customer lists', () => {
  it('shows the right visibility and archive controls in personal and organization workspaces', () => {
    cy.resetDB();
    cy.loginAndVisit('/admin/customer-lists');
    cy.get('[data-cy=btn-new]').should('be.visible');

    cy.get('[data-cy=btn-new]').click();
    cy.get('select[name=visibility]').should('have.value', 'private');
    cy.get('select[name=visibility] option[value=organization]').should('not.exist');
    cy.get('input[name=status]').should('not.exist');
    cy.get('input[name=name]').clear().type('Personal Cypress list');
    cy.get('[data-cy=btn-save]').click();
    cy.contains('tbody tr', 'Personal Cypress list').should('exist');

    cy.contains('tbody tr', 'Personal Cypress list').find('[data-cy=btn-edit]').click();
    cy.get('select[name=visibility]').should('have.value', 'private');
    cy.get('input[name=status]').should('exist');
    cy.get('.modal-card-foot button').first().click();

    cy.request('/api/profile').then(({ body }) => cy.request({
      method: 'POST',
      url: '/api/organizations',
      body: { name: 'Cypress organization', manager_user_id: body.data.id },
    })).then(({ body }) => {
      const organizationID = body.data.id;
      cy.window().then((win) => {
        win.localStorage.setItem('listmonk.workspace.organizationId', String(organizationID));
      });
      cy.setCookie('listmonk_workspace_organization_id', String(organizationID));
      cy.reload();
      cy.get('[data-cy=btn-new]').should('be.visible').click();
      cy.get('select[name=visibility] option[value=organization]').should('exist');
      cy.get('select[name=visibility]').should('have.value', 'organization');
      cy.get('input[name=status]').should('not.exist');
      cy.get('input[name=name]').clear().type('Shared Cypress list');
      cy.get('[data-cy=btn-save]').click();
      cy.contains('tbody tr', 'Shared Cypress list').should('exist');
      cy.contains('tbody tr', 'Shared Cypress list').find('[data-cy=btn-edit]').click();
      cy.get('select[name=visibility]').should('have.value', 'organization');
      cy.get('input[name=status]').should('exist');
      cy.get('select[name=visibility]').select('private');
      cy.get('[data-cy=btn-save]').click();
      cy.contains('tbody tr', 'Shared Cypress list').find('[data-cy=btn-edit]').click();
      cy.get('select[name=visibility]').should('have.value', 'private');
    });
  });
});
