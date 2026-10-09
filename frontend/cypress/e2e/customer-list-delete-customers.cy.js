/* eslint-env mocha */
/* global cy, expect */

describe('Delete customer lists and their unshared customers', () => {
  const continueDeletion = () => cy.contains('.dialog.is-active button', 'Continue').click();
  const deleteRow = (name) => cy.contains('tbody tr', name).find('[data-cy=btn-delete]').click();
  const createList = (name, type = 'private') => cy.request('POST', '/api/customer-lists', { name, type, optin: 'single' }).then(({ body }) => body.data.id);
  const createCustomer = (code, lists) => cy.request('POST', '/api/customers', {
    customer_code: code, name: code, email: `${code}@example.com`, customer_list_ids: lists,
  }).then(({ body }) => body.data.id);

  beforeEach(() => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/customer-lists');
  });

  it('cancels both confirmations, keeps customers on list-only deletion and protects shared customers in bulk deletion', () => {
    let first;
    let second;
    let keep;
    createList('Delete first').then((id) => { first = id; });
    createList('Delete second').then((id) => { second = id; });
    createList('Keep shared').then((id) => { keep = id; });
    createList('Lists only').then((id) => createCustomer('preserved', [id]));
    cy.then(() => createCustomer('unique', [first]));
    cy.then(() => createCustomer('both', [first, second]));
    cy.then(() => createCustomer('shared', [first, keep]));
    cy.then(() => cy.request(`/api/customers?customer_list_id=${first}`)).its('body.data.total').should('eq', 3);
    cy.visit('/admin/customer-lists');
    let requests = 0;
    cy.intercept('DELETE', /\/api\/customer-lists/, (req) => { requests += 1; req.continue(); }).as('deleteLists');
    deleteRow('Lists only');
    cy.contains('.dialog.is-active button', 'Cancel').click();
    cy.get('.dialog.is-active').should('not.exist');
    cy.get('[data-cy=list-delete-choice]').should('not.exist');
    deleteRow('Lists only');
    continueDeletion();
    cy.get('[data-cy=list-delete-choice]').should('be.visible');
    cy.get('[data-cy=list-delete-cancel]').should('be.focused').click();
    cy.get('[data-cy=list-delete-choice]').should('not.exist');
    cy.then(() => expect(requests).to.eq(0));
    cy.contains('tbody tr', 'Lists only').should('exist');
    deleteRow('Lists only');
    continueDeletion();
    cy.get('[data-cy=list-delete-only]').click();
    cy.wait('@deleteLists').then(({ request, response }) => {
      expect(request.url).to.contain('delete_customers=false');
      expect(response.statusCode).to.eq(200);
    });
    cy.request('/api/customers?search=preserved@example.com').its('body.data.total').should('eq', 1);
    cy.contains('tbody tr', 'Delete first').find('td.checkbox-cell label').click();
    cy.contains('tbody tr', 'Delete second').find('td.checkbox-cell label').click();
    cy.get('[data-cy=btn-delete-customer_lists]').first().click();
    continueDeletion();
    cy.get('[data-cy=list-delete-choice]').should('contain', 'these 2 customer lists');
    cy.viewport(1440, 950);
    cy.get('[data-cy=list-delete-choice]').screenshot('delete-lists-customer-choice-desktop');
    cy.viewport(390, 844);
    cy.get('[data-cy=list-delete-with-customers]').should('be.visible');
    cy.get('[data-cy=list-delete-choice]').then(($modal) => {
      const bounds = $modal[0].getBoundingClientRect();
      expect(bounds.left).to.be.at.least(0);
      expect(bounds.right).to.be.at.most(390);
    });
    cy.screenshot('delete-lists-customer-choice-mobile', { capture: 'viewport' });
    cy.get('[data-cy=list-delete-with-customers]').click();
    cy.wait('@deleteLists').its('response.statusCode').should('eq', 200);
    ['unique', 'both'].forEach((code) => {
      cy.request(`/api/customers?search=${code}@example.com`).its('body.data.total').should('eq', 0);
    });
    cy.request('/api/customers?search=shared@example.com').its('body.data.total').should('eq', 1);
    cy.request('/api/customer-lists?query=Keep%20shared').its('body.data.total').should('eq', 1);
  });

  it('deletes unshared public-pool contacts through the second confirmation', () => {
    let pool;
    createList('Delete public pool', 'pool').then((id) => {
      pool = id;
      cy.request('POST', `/api/customer-lists/${id}/pool-contacts`, {
        customer_code: 'POOL', name: 'Pool customer', email: 'pool@example.com',
      });
    });
    cy.visit('/admin/pool-lists');
    deleteRow('Delete public pool');
    continueDeletion();
    cy.intercept('DELETE', '/api/customer-lists/*').as('deletePool');
    cy.get('[data-cy=list-delete-with-customers]').click();
    cy.wait('@deletePool').its('response.statusCode').should('eq', 200);
    cy.request('/api/pools/contacts?search=pool@example.com').its('body.data.total').should('eq', 0);
    cy.then(() => cy.request({ url: `/api/customer-lists/${pool}`, failOnStatusCode: false })).its('status').should('eq', 404);
  });

  it('disables customer deletion without its permission and retains the choice after a failed list deletion', () => {
    createList('Permission list');
    cy.intercept('GET', '/api/profile', (req) => req.continue((res) => {
      res.body.data.user_role = { id: 77, permissions: ['workspaces:personal', 'customers:get', 'customer_lists:manage_all', 'customer_lists:delete'] };
    }));
    cy.visit('/admin/customer-lists');
    deleteRow('Permission list');
    continueDeletion();
    cy.get('[data-cy=list-delete-with-customers]').should('be.disabled');
    cy.get('[data-cy=list-delete-permission-help]').should('be.visible');
    cy.intercept({ method: 'DELETE', url: '/api/customer-lists/*', times: 1 }, { statusCode: 500, body: { message: 'Try again' } }).as('failedDeletion');
    cy.get('[data-cy=list-delete-only]').click();
    cy.wait('@failedDeletion');
    cy.get('[data-cy=list-delete-choice]').should('be.visible');
    cy.get('[data-cy=list-delete-only]').should('not.be.disabled');
    cy.intercept('DELETE', '/api/customer-lists/*').as('retryDeletion');
    cy.get('[data-cy=list-delete-only]').click();
    cy.wait('@retryDeletion').its('response.statusCode').should('eq', 200);
    cy.get('[data-cy=list-delete-choice]').should('not.exist');
  });
});
