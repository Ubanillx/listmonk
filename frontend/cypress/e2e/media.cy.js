describe('Media folders', () => {
  it('creates, navigates, nests and removes empty folders', () => {
    cy.resetDB();
    cy.loginAndVisit('/admin/campaigns/media');

    const rootName = 'media-folder-test';
    const childName = 'media-folder-child-test';

    cy.get('[data-cy=btn-create-folder]').click();
    cy.get('.modal input').type(rootName);
    cy.get('.modal button.is-primary').click();
    cy.get('[data-cy=media-folder]').contains(rootName).click();

    cy.get('.media-breadcrumb').contains(rootName).should('have.class', 'is-active');
    cy.get('[data-cy=btn-create-folder]').click();
    cy.get('.modal input').type(childName);
    cy.get('.modal button.is-primary').click();
    cy.get('[data-cy=media-folder]').contains(childName).should('be.visible');

    cy.get('[data-cy=media-folder]').contains(childName)
      .parents('[data-cy=media-folder]')
      .find('[data-cy=btn-delete-folder]')
      .click();
    cy.get('.modal button.is-primary').click();
    cy.get('[data-cy=media-folder]').contains(childName).should('not.exist');

    cy.get('.media-breadcrumb a').first().click();
    cy.get('[data-cy=media-folder]').contains(rootName)
      .parents('[data-cy=media-folder]')
      .find('[data-cy=btn-delete-folder]')
      .click();
    cy.get('.modal button.is-primary').click();
    cy.get('[data-cy=media-folder]').contains(rootName).should('not.exist');
  });
});
