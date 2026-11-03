/* eslint-env mocha */
/* global cy, expect */

describe('SMTP quick configuration', () => {
  it('fills only the selected personal server and persists its connection settings', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/user/profile');
    cy.request('PUT', '/api/profile/smtp', {
      smtp: ['primary', 'backup'].map((name) => ({
        name, enabled: true, host: 'host.docker.internal', port: 6125,
        auth_protocol: 'none', tls_type: 'none', from_email: 'sender@example.invalid', daily_limit: 20,
      })),
    });
    cy.reload();
    cy.get('.user-profile .b-tabs nav a').eq(1).click();
    cy.get('.personal-smtp .smtp-card').should('have.length', 2).first().within(() => {
      cy.get('select').first().select('plain');
      cy.get('input[placeholder="user@example.com"]').type('old@example.invalid');
      cy.get('input[type=password]').type('old-password');
      cy.get('input[placeholder="mail.example.com"]').type('old.example.invalid');
      cy.get('[data-cy=smtp-presets] button').should('have.length', 10);
      cy.contains('[data-cy=smtp-presets] button', '263net').click();
      cy.get('input[placeholder="smtp.example.com"]').should('have.value', 'smtp.263.net');
      cy.get('input[type=number]').first().should('have.value', '465');
      cy.get('select').first().should('have.value', 'login');
      cy.get('[data-cy=account-smtp-tls-type]').should('have.value', 'TLS');
      cy.get('input[placeholder="user@example.com"]').should('have.value', '').and('be.focused');
      cy.get('input[type=password]').should('have.value', '');
      cy.get('input[placeholder="mail.example.com"]').should('have.value', '');
      cy.get('input[placeholder="sender@example.com"]').should('have.value', 'sender@example.invalid');
      cy.get('input[type=number]').eq(1).should('have.value', '20');
      cy.contains('[data-cy=smtp-presets] button', 'Postmark').click();
      cy.get('input[type=number]').first().should('have.value', '587');
      cy.get('select').first().should('have.value', 'cram');
      cy.get('[data-cy=account-smtp-tls-type]').should('have.value', 'STARTTLS');
      cy.contains('[data-cy=smtp-presets] button', 'topmax').click();
      cy.get('input[placeholder="smtp.example.com"]').should('have.value', 'smtp.topmax.cn');
      cy.get('[data-cy=account-smtp-tls-type]').should('have.value', 'TLS');
      cy.get('input[placeholder="user@example.com"]').type('new@example.invalid');
      cy.get('input[type=password]').type('new-password');
    });
    cy.get('.personal-smtp .smtp-card').eq(1).within(() => {
      cy.get('input[placeholder="smtp.example.com"]').should('have.value', 'host.docker.internal');
      cy.get('input[type=number]').first().should('have.value', '6125');
      cy.get('select').first().should('have.value', 'none');
      cy.get('[data-cy=account-smtp-tls-type]').should('have.value', 'none');
    });
    cy.intercept('PUT', '/api/profile/smtp').as('saveSMTP');
    cy.get('.smtp-save-bar button').click();
    cy.wait('@saveSMTP').then(({ request, response }) => {
      expect(response.statusCode).to.equal(200);
      expect(request.body.smtp[0]).to.include({
        name: 'primary', host: 'smtp.topmax.cn', port: 465, auth_protocol: 'login', tls_type: 'TLS',
        username: 'new@example.invalid', password: 'new-password', from_email: 'sender@example.invalid', daily_limit: 20,
      });
      expect(request.body.smtp[1].host).to.equal('host.docker.internal');
    });
    cy.reload();
    cy.get('.user-profile .b-tabs nav a').eq(1).click();
    cy.get('.personal-smtp .smtp-card').first().find('input[placeholder="smtp.example.com"]')
      .should('have.value', 'smtp.topmax.cn');
    cy.get('.personal-smtp .smtp-card').first().screenshot('personal-smtp-presets-desktop');
    cy.get('.add-smtp-button').click();
    cy.get('.personal-smtp .smtp-card').should('have.length', 3).last().within(() => {
      cy.contains('[data-cy=smtp-presets] button', '263net').click();
      cy.get('input[placeholder="smtp.example.com"]').should('have.value', 'smtp.263.net');
      cy.get('input[placeholder="user@example.com"]').should('be.focused');
    });
    cy.viewport(390, 844);
    cy.get('.personal-smtp .smtp-card').first().within(() => {
      cy.get('[data-cy=smtp-presets] button').should('be.visible');
      cy.contains('[data-cy=smtp-presets] button', 'Gmail').click();
      cy.get('input[placeholder="smtp.example.com"]').should('have.value', 'smtp.gmail.com');
    });
    cy.window().then((win) => expect(win.document.documentElement.scrollWidth).to.be.at.most(win.innerWidth));
    cy.get('.personal-smtp .smtp-card').first().screenshot('personal-smtp-presets-mobile');
    cy.viewport(1400, 950);
  });

  it('keeps the settings page presets working', () => {
    cy.visit('/admin/settings');
    cy.get('.b-tabs nav a').eq(5).click();
    cy.get('.settings-smtp .mail-servers').within(() => {
      cy.get('input[name=username]').clear().type('old@example.invalid');
      cy.contains('[data-cy=smtp-presets] button', 'Postmark').click();
      cy.get('input[name=host]').should('have.value', 'smtp.postmarkapp.com');
      cy.get('input[name=port]').should('have.value', '587');
      cy.get('select[name=auth_protocol]').should('have.value', 'cram');
      cy.get('[data-cy=system-smtp-tls-type]').should('have.value', 'STARTTLS');
      cy.get('input[name=username]').should('have.value', '').and('be.focused');
      cy.get('input[name=password]').should('have.value', '');
      cy.contains('[data-cy=smtp-presets] button', 'topmax').click();
      cy.get('input[name=host]').should('have.value', 'smtp.topmax.cn');
      cy.get('input[name=port]').should('have.value', '465');
      cy.get('[data-cy=system-smtp-tls-type]').should('have.value', 'TLS');
    });
  });
});
