/* eslint-env mocha */
/* global cy, Cypress */

// Opt-in, non-destructive smoke test. Supply an administrator test account.
// Does not run the default suite's destructive database initialization.
describe('Data exports', function exportSuite() {
  before(function requireExportFixture() {
    if (!Cypress.env('EXPORT_E2E')) this.skip();
    cy.request('/admin/login').then((response) => {
      const nonce = /name="nonce" value="([^"]+)"/.exec(response.body)[1];
      return cy.request({ method: 'POST', url: '/admin/login', form: true, body: {
        username: Cypress.env('EXPORT_QA_USER'), password: Cypress.env('EXPORT_QA_PASSWORD'), nonce, next: '/admin',
      } });
    });
    cy.setCookie('listmonk_workspace_organization_id', '0');
    cy.visit('/admin/customers', { onBeforeLoad(win) { win.localStorage.setItem('listmonk.workspace.organizationId', '0'); } });
  });

  it('creates a filtered CSV job and downloads the completed file', () => {
    cy.get('[data-cy=export-center]').should('not.exist');
    cy.get('[data-cy=export-open]').click();
    cy.get('.modal-card select').eq(0).select('customers');
    cy.get('.modal-card select').eq(1).select('csv');
    cy.get('.modal-card-body input[maxlength="500"]').type('export-ui-no-match-52aadcd4');
    cy.intercept('POST', '/api/exports').as('createExport');
    cy.get('[data-cy=export-submit]').click();
    cy.wait('@createExport').then(({ request, response }) => {
      expect(request.body.search).to.equal('export-ui-no-match-52aadcd4');
      expect(response.statusCode).to.equal(202);
      const id = response.body.data.id;
      cy.get('[data-cy=export-download]', { timeout: 25000 }).should('be.visible');
      cy.location('pathname').should('eq', '/admin/customers');
      cy.contains('.modal-card-foot button', '关闭').click();
      cy.get('[data-cy=export-open]').click();
      cy.get('[data-cy=export-download]').should('be.visible');
      cy.request(`/api/exports/${id}/download?organization_id=0`).then((download) => {
        expect(download.status).to.equal(200);
        expect(download.headers['content-disposition']).to.include('attachment');
        expect(download.body).to.include('客户编码');
        expect(download.body.trim().split('\n')).to.have.length(1);
      });
    });
    cy.screenshot('export-dialog', { capture: 'viewport' });
  });

  it('exports pool removal reasons without a separate private conversion type', () => {
    cy.visit('/admin/customer-lists');
    cy.get('[data-cy=export-open]').click();
    cy.get('.modal-card select').eq(0).find('option[value="pool_private"]').should('not.exist');
    cy.get('.modal-card select').eq(0).select('pools');
    cy.get('.modal-card select').eq(0).should('have.value', 'pools');
    cy.screenshot('pool-export-types', { capture: 'viewport' });
  });

});
