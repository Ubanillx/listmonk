/* eslint-env mocha */
/* global cy, expect */

describe('Account SMTP test feedback', () => {
  it('shows authentication failures, clears loading, and allows a successful retry', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/user/profile');
    cy.request('PUT', '/api/profile/smtp', {
      smtp: [{
        name: 'Feedback regression',
        enabled: true,
        host: 'host.docker.internal',
        port: 6125,
        auth_protocol: 'none',
        tls_type: 'none',
        from_email: 'smtp-feedback@example.invalid',
      }],
    });
    cy.reload();
    cy.get('.user-profile .b-tabs nav a').eq(1).click();
    cy.get('.personal-smtp .smtp-card').should('have.length', 1);
    cy.get('.smtp-test-controls input').type('smtp-recipient@example.invalid');
    cy.intercept('POST', '/api/profile/smtp/test', {
      statusCode: 500, body: { message: '535 Authentication credentials invalid' }, delay: 500,
    }).as('failedTest');
    cy.get('.smtp-test-controls button').click();
    cy.get('.smtp-test-controls button').should('have.class', 'is-loading');
    cy.wait('@failedTest');
    cy.get('[data-cy=account-smtp-test-result]').should('be.visible').and('contain', '535 Authentication credentials invalid');
    cy.get('.smtp-test-controls button').should('not.have.class', 'is-loading').and('not.be.disabled');
    cy.screenshot('smtp-test-authentication-failure');
    cy.viewport(390, 844);
    cy.get('[data-cy=account-smtp-test-result]').should('be.visible');
    cy.window().then((win) => expect(win.document.documentElement.scrollWidth).to.be.at.most(win.innerWidth));
    cy.screenshot('smtp-test-authentication-failure-mobile');
    cy.viewport(1400, 950);
    cy.intercept('POST', '/api/profile/smtp/test', (req) => req.continue()).as('successfulTest');
    cy.get('.smtp-test-controls button').click();
    cy.wait('@successfulTest').its('response.statusCode').should('eq', 200);
    cy.get('[data-cy=account-smtp-test-result]').should('contain', 'Test message sent')
      .and('not.contain', 'Authentication credentials invalid');
    cy.get('.smtp-test-controls button').should('not.have.class', 'is-loading').and('not.be.disabled');
    cy.screenshot('smtp-test-success');
    cy.intercept('POST', '/api/profile/smtp/test', { forceNetworkError: true }).as('networkFailure');
    cy.get('.smtp-test-controls button').click();
    cy.wait('@networkFailure');
    cy.get('[data-cy=account-smtp-test-result]').should('be.visible').and('contain', 'Network Error');
    cy.get('.smtp-test-controls button').should('not.have.class', 'is-loading').and('not.be.disabled');
  });

  it('shows organization SMTP failures with a single notification', () => {
    let organizationID;
    let poolID;
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'SMTP feedback team', manager_user_id: body.data.id,
    })).then(({ body }) => { organizationID = body.data.id; });
    cy.then(() => cy.request({
      method: 'POST',
      url: '/api/organizations/smtp-pools',
      headers: { 'X-Listmonk-Organization-ID': organizationID },
      body: { name: 'Feedback senders' },
    })).then(({ body }) => { poolID = body.data.id; });
    cy.then(() => cy.request({
      method: 'PUT',
      url: `/api/organizations/smtp?pool_id=${poolID}`,
      headers: { 'X-Listmonk-Organization-ID': organizationID },
      body: {
        smtp: [{
          name: 'Organization sender',
          enabled: true,
          host: 'host.docker.internal',
          port: 6125,
          auth_protocol: 'none',
          tls_type: 'none',
          from_email: 'organization-feedback@example.invalid',
        }],
      },
    }));
    cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(organizationID)));
    cy.then(() => cy.setCookie('listmonk_workspace_organization_id', String(organizationID)));
    cy.visit('/admin/organizations/manage');
    cy.then(() => cy.get('.section-mini select').select(String(organizationID)));
    cy.get('.b-tabs nav a').contains('SMTP').click();
    cy.get('[data-cy=organization-smtp] .smtp-test-controls input').type('organization-recipient@example.invalid');
    cy.intercept('POST', '/api/organizations/smtp/test*', {
      statusCode: 500, body: { message: 'Organization SMTP connection refused' },
    }).as('organizationFailure');
    cy.get('[data-cy=organization-smtp] .smtp-test-controls button').click();
    cy.wait('@organizationFailure');
    cy.get('[data-cy=account-smtp-test-result]').should('contain', 'Organization SMTP connection refused');
    cy.get('.toast').filter(':contains("Organization SMTP connection refused")').should('have.length', 1);
    cy.get('.smtp-test-controls button').should('not.have.class', 'is-loading').and('not.be.disabled');
  });
});
