/* eslint-env mocha */
/* global cy, expect */

describe('Administrator campaign confirmation', () => {
  it('requires an explicit confirmation before saving another owner\'s campaign', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/campaigns');
    cy.request('POST', '/api/campaigns', {
      name: 'Other owner confirmation',
      subject: 'Original subject',
      type: 'regular',
      content_type: 'plain',
      body: 'Test body',
      customer_list_ids: [1],
      smtp_source: 'personal',
      smtp_rate_limit: 20,
    }).then(({ body }) => {
      const { id } = body.data;
      let writes = 0;
      cy.intercept('GET', `/api/campaigns/${id}`, (req) => req.continue((res) => {
        res.body.data.owner_user_id = 999;
        res.body.data.owner_name = 'Other owner';
      }));
      cy.intercept('PUT', `/api/campaigns/${id}`, (req) => {
        writes += 1;
        req.continue();
      }).as('saveOtherCampaign');
      cy.visit(`/admin/campaigns/${id}`);
      cy.get('input[name=subject]').clear().type('Confirmed subject');
      cy.get('.campaign > header [data-cy=btn-save]').click();
      cy.get('.dialog.is-active').should('contain', 'Other owner').and('contain', 'Other owner confirmation');
      cy.then(() => expect(writes).to.eq(0));
      cy.get('.dialog.is-active .modal-card-foot button').first().click();
      cy.get('.dialog.is-active').should('not.exist');
      cy.then(() => expect(writes).to.eq(0));
      cy.get('.campaign > header [data-cy=btn-save]').click();
      cy.contains('.dialog.is-active button', 'Ok').click();
      cy.wait('@saveOtherCampaign').its('response.statusCode').should('eq', 200);
      cy.then(() => expect(writes).to.eq(1));
      cy.request(`/api/campaigns/${id}`).its('body.data.subject').should('eq', 'Confirmed subject');
    });
  });
});
