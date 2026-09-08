/* eslint-env mocha */
/* global cy, Cypress */

// Opt-in against the existing development stack; never resets or saves data.
describe('Bounce mailbox diagnostics', function bounceMailboxSuite() {
  before(function enabled() {
    if (!Cypress.env('BOUNCE_E2E')) this.skip();
    cy.request('/admin/login').then((response) => {
      const nonce = /name="nonce" value="([^"]+)"/.exec(response.body)[1];
      cy.request({
        method: 'POST', url: '/admin/login', form: true,
        body: { username: Cypress.env('BOUNCE_QA_USER') || 'root', password: Cypress.env('BOUNCE_QA_PASSWORD') || 'Test@1234', nonce, next: '/admin' },
      });
    });
    cy.request('/api/settings').its('status').should('eq', 200);
  });

  beforeEach(() => {
    cy.intercept('GET', '/api/settings', (req) => req.continue((res) => {
      res.body.data['bounce.enabled'] = true;
      res.body.data['bounce.mailboxes'] = [{
        enabled: true, type: 'pop', host: 'pop.example.com', port: 995, auth_protocol: 'userpass',
        username: 'bounce@example.com', password: '••••', uuid: 'test-mailbox', tls_enabled: true, scan_interval: '15m',
      }];
    }));
    cy.visit('/admin/settings');
    cy.get('.b-tabs nav a').contains(/Bounces|退信/).click();
  });

  it('Shows parsed mail and four steps without saving settings', () => {
    let saves = 0;
    cy.intercept('PUT', '/api/settings', () => { saves += 1; });
    cy.intercept('POST', '/api/settings/bounce/mailbox/test', {
      delay: 300,
      body: { data: {
        status: 'success', count: 3,
        steps: ['connect', 'login', 'read', 'parse'].map((name) => ({ name, status: 'success' })),
        message: {
          from: 'Mailer <mailer@example.com>', subject: '退信通知', date: '2026-09-08T11:00:00+08:00', date_source: 'received',
          is_bounce: true, bounce_type: 'hard', reason: '550 user unknown',
          recipients: [{ address: 'failed@example.com', source: 'final-recipient', status: '5.1.1', reason: '550 user unknown' }],
        },
      } },
    }).as('testMailbox');
    cy.get('[data-cy=test-bounce-mailbox]').click().should('be.disabled');
    cy.wait('@testMailbox').its('request.body').should('include', { host: 'pop.example.com', scan_interval: '15m', uuid: 'test-mailbox' });
    cy.get('[data-cy=bounce-test-result]').should('contain', 'failed@example.com').and('contain', '5.1.1').and('contain', '退信通知');
    cy.get('[data-cy=test-bounce-mailbox]').should('not.be.disabled');
    cy.then(() => expect(saves).to.equal(0));
  });

  it('Distinguishes empty, ordinary mail, and authentication failure', () => {
    ['empty', 'not_bounce', 'failed'].forEach((status) => {
      cy.intercept('POST', '/api/settings/bounce/mailbox/test', { body: { data: {
        status, count: status === 'empty' ? 0 : 1,
        steps: [{ name: 'connect', status: 'success' }, { name: 'login', status: status === 'failed' ? 'failed' : 'success', detail: status === 'failed' ? 'POP server: invalid login' : '' }],
      } } });
      cy.get('[data-cy=test-bounce-mailbox]').click();
      cy.get('[data-cy=bounce-test-result] .notification').should('have.class', status === 'failed' ? 'is-danger' : 'is-warning');
      if (status === 'failed') cy.get('[data-cy=bounce-test-result]').should('contain', 'invalid login');
    });
  });
});
