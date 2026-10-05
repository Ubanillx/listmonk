const apiUrl = Cypress.env('apiUrl');
describe('Settings', () => {
  it('separates SMTP presets from the HELO field on desktop and mobile', () => {
    cy.resetDB();
    cy.loginAndVisit('/admin/settings');
    cy.get('.b-tabs nav a').eq(5).click();

    [1795, 390].forEach((width) => {
      cy.viewport(width, 1244);
      cy.get('.smtp-presets').should(($presets) => {
        const presets = $presets[0].getBoundingClientRect();
        const label = $presets[0].nextElementSibling.querySelector('label').getBoundingClientRect();
        expect(label.top - presets.bottom, 'gap between presets and HELO label').to.be.at.least(8);
        $presets[0].querySelectorAll('a').forEach((link) => {
          const rect = link.getBoundingClientRect();
          expect(rect.left).to.be.at.least(presets.left - 1);
          expect(rect.right).to.be.at.most(presets.right + 1);
        });
      });
      cy.get('.mail-servers .box').screenshot(`smtp-presets-${width}`);
    });
    cy.viewport(1400, 950);
  });

  it('keeps one system SMTP and saves shared delivery and random delay options', () => {
    cy.resetDB();
    cy.loginAndVisit('/admin/settings');
    cy.get('.b-tabs nav a').eq(5).click();
    cy.get('.tab-item:visible .mail-servers .box').should('have.length', 1);
    cy.get('[data-cy=smtp-delivery-settings]').should('be.visible');
    cy.get('[data-cy=system-smtp-tls]').should('be.visible');
    cy.get('[data-cy=smtp-delivery-settings]').find('select, input[type=checkbox]').should('not.exist');
    cy.get('[data-cy=system-smtp-tls-type]').select('STARTTLS');
    cy.get('[data-cy=smtp-delivery-max-conns]').clear().type('12');
    cy.get('[data-cy=smtp-send-delay-min]').should('have.value', '0').clear().type('2000');
    cy.get('[data-cy=smtp-send-delay-max]').should('have.value', '0').clear().type('5000');
    cy.intercept('PUT', '**/api/settings').as('saveSettings');
    cy.get('[data-cy=btn-save]').click();
    cy.wait('@saveSettings');
    cy.get('[data-cy=btn-save]', { timeout: 60000 }).should('be.disabled');

    cy.request(`${apiUrl}/api/settings`).should((response) => {
      const { data } = response.body;
      expect(data.smtp).to.have.length(1);
      expect(data.smtp[0].enabled).to.equal(true);
      expect(data.smtp_delivery.max_conns).to.equal(12);
      expect(data.smtp[0].tls_type).to.equal('STARTTLS');
      expect(data.smtp_delivery).not.to.have.property('tls_type');
      expect(data.smtp_delivery.send_delay_min).to.equal(2000);
      expect(data.smtp_delivery.send_delay_max).to.equal(5000);
    });
    cy.visit('/admin/settings');
    cy.get('.b-tabs nav a').eq(5).click();
    cy.get('[data-cy=system-smtp-tls-type]').should('have.value', 'STARTTLS');
    cy.get('[data-cy=smtp-send-delay-min]').should('have.value', '2000');
    cy.get('[data-cy=smtp-send-delay-max]').should('have.value', '5000');
    cy.get('[data-cy=smtp-delivery-settings]').screenshot('smtp-random-delay');
    cy.viewport(390, 1244);
    cy.get('[data-cy=smtp-send-delay-min]').should('be.visible');
    cy.get('[data-cy=smtp-send-delay-max]').should('be.visible');
    cy.get('[data-cy=smtp-delivery-settings]').screenshot('smtp-random-delay-mobile');
    cy.viewport(1400, 950);
    cy.request(`${apiUrl}/api/settings`).then(({ body }) => {
      const data = body.data;
      data.smtp_delivery.send_delay_min = 6000;
      cy.request({ method: 'PUT', url: `${apiUrl}/api/settings`, body: data, failOnStatusCode: false })
        .its('status').should('equal', 400);
      cy.request({ method: 'PUT', url: `${apiUrl}/api/settings/smtp_delivery`, body: data.smtp_delivery, failOnStatusCode: false })
        .its('status').should('equal', 400);
    });
  });
});
