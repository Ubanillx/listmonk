/* eslint-env mocha */
/* global cy, expect */

describe('Campaign cloning template references', () => {
  beforeEach(() => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/campaigns');
  });

  it('clones a campaign twice without adding templates to the library', () => {
    let templateID;
    let templateCount;
    cy.request('POST', '/api/templates', {
      name: 'Reusable clone layout', type: 'campaign', body: '<p>Reusable header</p>{{ template "content" . }}',
    }).then(({ body }) => {
      templateID = body.data.id;
      return cy.request('POST', '/api/campaigns', {
        name: 'Clone source',
        subject: 'Clone source',
        type: 'regular',
        content_type: 'html',
        body: '<p>Cloned content</p>',
        template_id: templateID,
        customer_list_ids: [1],
      });
    });
    cy.request('/api/templates').then(({ body }) => { templateCount = body.data.length; });

    const cloneFromList = (sourceName, cloneName) => {
      cy.visit('/admin/campaigns');
      cy.contains('tbody tr', sourceName).find('[data-cy=btn-clone]').click();
      cy.get('.modal.is-active input').clear().type(cloneName);
      cy.intercept('POST', '/api/campaigns/*/clone').as('clone');
      cy.get('.modal.is-active .modal-card-foot button.is-primary').click();
      cy.wait('@clone').then(({ response }) => {
        expect(response.statusCode).to.eq(201);
        expect(response.body.data).to.include({ template_id: templateID, status: 'draft' });
        cy.location('pathname').should('eq', `/admin/campaigns/${response.body.data.id}`);
        cy.request(`/api/campaigns/${response.body.data.id}/preview`).its('body')
          .should('contain', 'Reusable header').and('contain', 'Cloned content');
      });
      cy.request('/api/templates').then(({ body }) => {
        expect(body.data).to.have.length(templateCount);
        expect(body.data.filter((template) => template.name === 'Reusable clone layout')).to.have.length(1);
        expect(body.data.some((template) => template.name === 'Reusable clone layout (copy)')).to.eq(false);
      });
    };

    cloneFromList('Clone source', 'First clone');
    cloneFromList('First clone', 'Second clone');
    cy.visit('/admin/campaigns/templates');
    cy.contains('tbody tr', 'Reusable clone layout').should('be.visible');
    cy.contains('tbody tr', 'Reusable clone layout (copy)').should('not.exist');
  });

  it('keeps an independent template when copying into another workspace', () => {
    let templateID;
    let campaignID;
    cy.request('POST', '/api/templates', {
      name: 'Private source layout', type: 'campaign', body: '<p>Cross-workspace header</p>{{ template "content" . }}',
    }).then(({ body }) => {
      templateID = body.data.id;
      return cy.request('POST', '/api/campaigns', {
        name: 'Cross-workspace source',
        subject: 'Clone',
        type: 'regular',
        content_type: 'html',
        body: '<p>Cross-workspace content</p>',
        template_id: templateID,
        customer_list_ids: [1],
      });
    }).then(({ body }) => { campaignID = body.data.id; });
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Clone destination', manager_user_id: body.data.id,
    })).then(({ body }) => {
      const organizationID = body.data.id;
      cy.request('POST', `/api/campaigns/${campaignID}/clone`, {
        name: 'Cross-workspace clone', target_organization_id: organizationID,
      }).then(({ body: cloneBody }) => {
        expect(cloneBody.data.template_id).not.to.eq(templateID);
        expect(cloneBody.data.organization_id).to.eq(organizationID);
        const headers = { 'X-Listmonk-Organization-ID': organizationID };
        cy.request({ url: `/api/templates/${cloneBody.data.template_id}`, headers }).then(({ body: templateBody }) => {
          expect(templateBody.data).to.include({ name: 'Private source layout (copy)', visibility: 'private', organization_id: organizationID });
        });
        cy.request({ url: `/api/campaigns/${cloneBody.data.id}/preview`, headers }).its('body')
          .should('contain', 'Cross-workspace header').and('contain', 'Cross-workspace content');
      });
    });
  });
});
