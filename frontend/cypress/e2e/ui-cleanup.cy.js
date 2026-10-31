/* eslint-env mocha */
/* global cy, expect */

describe('UI cleanup', () => {
  before(() => { cy.resetDB(); });
  beforeEach(() => {
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
  });

  it('protects new campaign input but lets an untouched form leave', () => {
    cy.loginAndVisit('/admin/campaigns/new');
    cy.get('[data-cy=campaign-save-first]').should('be.visible');
    cy.get('.menu [data-cy=user-profile]').click();
    cy.location('pathname').should('eq', '/admin/user/profile');
    cy.get('.dialog').should('not.exist');
    cy.visit('/admin/campaigns/new');
    cy.get('input[name=name]').type('Unsaved campaign');
    cy.get('input[name=subject]').type('Unsaved subject');
    cy.get('.menu [data-cy=user-profile]').click();
    cy.get('.dialog').should('be.visible').find('.button').contains('Cancel').click();
    cy.location('pathname').should('eq', '/admin/campaigns/new');
    cy.get('input[name=name]').should('have.value', 'Unsaved campaign');
    cy.get('input[name=subject]').should('have.value', 'Unsaved subject');
    cy.get('.menu [data-cy=user-profile]').click();
    cy.get('.dialog .button.is-warning').click();
    cy.location('pathname').should('eq', '/admin/user/profile');
  });

  it('keeps expanded select decorations within the input boundary', () => {
    cy.loginAndVisit('/admin/campaigns/templates');
    cy.get('.templates .btn-new').click();
    cy.viewport(1440, 900);
    cy.get('.modal-card .select').each(($select) => {
      const select = $select[0];
      const input = select.querySelector('select');
      expect(Math.abs(select.getBoundingClientRect().width - input.getBoundingClientRect().width)).to.be.lessThan(2);
    });
    cy.viewport(390, 844);
    cy.document().then((doc) => expect(doc.documentElement.scrollWidth).to.be.at.most(390));
    cy.get('.modal-card-foot .button').contains('Cancel').click();
  });

  it('shows upload before mapping and marks maintenance actions as destructive', () => {
    cy.loginAndVisit('/admin/customers/import');
    cy.get('input[type=file]').should('exist');
    cy.get('.import select').should('not.exist');
    cy.get('.import-help').should('not.have.attr', 'open');
    cy.intercept('GET', '/api/maintenance/customers/orphan/preview*', { body: { data: { count: 5 } } });
    cy.visit('/admin/settings/maintenance');
    cy.get('.maintenance .button.is-danger').should('have.length', 3);
    cy.get('.maintenance .button.is-danger').first().click();
    cy.get('.dialog').should('contain', 'cannot be undone');
    cy.get('.dialog .button').contains('Cancel').click();
    cy.get('.dialog').should('not.exist');
  });
});
