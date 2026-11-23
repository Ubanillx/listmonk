describe('Campaign template selection', () => {
  it('shows the template while creating, previews it and keeps the selection', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/campaigns/new');
    let listID;
    cy.request('POST', '/api/customer-lists', { name: 'Template recipients', type: 'private', optin: 'single' })
      .then(({ body }) => { listID = body.data.id; });
    cy.visit('/admin/campaigns/new');
    cy.viewport(1440, 1000);

    // The creation form surfaces the wrapping template with a usable default.
    cy.get('[data-cy=campaign-template-field]').should('be.visible');
    cy.get('[data-cy=campaign-template]').should('contain', 'Default campaign template');

    // Preview renders the selected template before the campaign is saved.
    cy.intercept('POST', '/api/campaigns/preview').as('draftPreview');
    cy.get('[data-cy=btn-preview-draft]').click();
    cy.wait('@draftPreview').its('response.statusCode').should('eq', 200);
    cy.get('.modal iframe').should(($iframe) => {
      expect($iframe.attr('srcdoc') || '').to.contain('<!doctype html>');
    });
    cy.get('.modal-card-foot button').click();

    // Create the campaign and verify the selected template was persisted.
    cy.then(() => {
      cy.get('[data-cy=campaign-audience-trigger]').click();
      cy.get(`[data-cy=campaign-audience-${listID}]`).check();
      cy.get('[data-cy=campaign-audience-trigger]').click();
    });
    cy.get('input[name=name]').type('Template selection campaign');
    cy.get('input[name=subject]').type('Template selection subject');
    cy.intercept('POST', '/api/campaigns').as('createCampaign');
    cy.get('[data-cy=btn-continue]').click();
    cy.wait('@createCampaign').then(({ response }) => {
      expect(response.statusCode).to.eq(200);
      expect(response.body.data.template_id).to.be.a('number');
    });
  });
});
