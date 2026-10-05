describe('Campaign configuration', () => {
  it('keeps common settings visible, saves optional settings and stacks on mobile', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/campaigns/new');
    let listID;
    cy.request('POST', '/api/customer-lists', { name: 'Setup recipients', type: 'private', optin: 'single' })
      .then(({ body }) => { listID = body.data.id; });
    cy.visit('/admin/campaigns/new');
    cy.viewport(1440, 1000);
    cy.get('[data-cy=campaign-basics]').should('be.visible');
    cy.get('[data-cy=campaign-audience-section]').should('be.visible');
    cy.get('[data-cy=campaign-sender-section]').should('be.visible');
    cy.get('[data-cy=campaign-delivery-section]').should('be.visible');
    cy.get('[data-cy=campaign-test-section]').should('not.exist');
    cy.get('[data-cy=campaign-advanced]').should('not.have.attr', 'open');
    cy.get('[data-cy=campaign-delivery-rules]').should('not.have.attr', 'open');
    cy.get('[data-cy=campaign-delivery-rules] summary').click();
    cy.get('[data-cy=campaign-delivery-rules]').should('contain', 'Platform and individual SMTP limits still apply');

    cy.get('[data-cy=campaign-advanced] summary').click();
    cy.get('input[name=tags]').type('setup-tag{enter}');
    cy.get('[data-cy=btn-headers]').click();
    cy.get('textarea[name=headers]').clear().type('[{"X-Setup": "saved"}]', { parseSpecialCharSequences: false });
    cy.get('[data-cy=btn-send-later] .check').click();
    cy.get('[data-cy=send_at]').should('be.visible');
    cy.get('[data-cy=btn-send-later] .check').click();
    cy.get('[data-cy=send_at]').should('not.exist');

    cy.then(() => {
      cy.get('[data-cy=campaign-audience-trigger]').click();
      cy.get(`[data-cy=campaign-audience-${listID}]`).check();
      cy.get('[data-cy=campaign-audience-trigger]').click();
    });
    cy.get('input[name=name]').type('Simple campaign setup');
    cy.get('input[name=subject]').type('Simple setup subject');
    cy.get('[data-cy=campaign-smtp-rate-limit]').clear().type('24');
    cy.intercept('POST', '/api/campaigns').as('createSetup');
    cy.get('[data-cy=btn-continue]').click();
    cy.wait('@createSetup').then(({ response }) => {
      expect(response.statusCode).to.eq(200);
      expect(response.body.data.smtp_rate_limit).to.eq(24);
      expect(response.body.data.auto_track_links).to.eq(true);
      expect(response.body.data.tags).to.deep.eq(['setup-tag']);
      expect(response.body.data.headers).to.deep.eq([{ 'X-Setup': 'saved' }]);
      cy.visit(`/admin/campaigns/${response.body.data.id}`);
    });
    cy.get('[data-cy=campaign-test-section]').should('be.visible');
    cy.get('[data-cy=campaign-advanced]').should('have.attr', 'open');
    cy.get('textarea[name=headers]').should('contain.value', 'X-Setup');
    cy.viewport(390, 844);
    cy.get('[data-cy=campaign-basics]').then(($basics) => {
      cy.get('[data-cy=campaign-sender-section]').then(($sender) => {
        expect($sender[0].getBoundingClientRect().top).to.be.greaterThan($basics[0].getBoundingClientRect().bottom);
      });
    });
    cy.document().then((doc) => expect(doc.documentElement.scrollWidth).to.be.at.most(390));
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '24');
  });
});
