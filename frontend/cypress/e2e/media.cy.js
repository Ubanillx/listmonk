describe('Media folders', () => {
  it('creates, navigates, nests and removes empty folders', () => {
    cy.resetDB();
    cy.loginAndVisit('/admin/campaigns/media');
    cy.request('POST', '/api/roles/users', {
      name: 'Folder member', permissions: ['media:get', 'media:manage', 'workspaces:personal'],
    }).then(({ body }) => cy.request('POST', '/api/users', {
      username: 'folder-member', name: 'Folder member', email: 'folder-member@example.com',
      type: 'user', status: 'enabled', password_login: true, password: 'folder-member-test', user_role_id: body.data.id,
    }));
    cy.clearCookies();
    cy.visit('/admin/login?next=/admin/campaigns/media');
    cy.get('input[name=username]').invoke('val', 'folder-member');
    cy.get('input[name=password]').invoke('val', 'folder-member-test');
    cy.get('button').click();

    const rootName = 'media-folder-test';
    const childName = 'media-folder-child-test';

    cy.get('[data-cy=btn-create-folder]').click();
    cy.get('.modal input').type(rootName);
    cy.get('select[data-cy=folder-permission]').should('have.value', 'private');
    cy.get('select[data-cy=folder-permission]').find('option[value=organization]').should('be.disabled');
    cy.get('.modal button.is-primary').click();
    cy.get('[data-cy=media-folder]').contains(rootName).click();

    cy.get('.media-breadcrumb').contains(rootName).parents('li').should('have.class', 'is-active');
    cy.get('[data-cy=btn-create-folder]').click();
    cy.get('.modal input').type(childName);
    cy.get('.modal button.is-primary').click();
    cy.get('[data-cy=media-folder]').contains(childName).should('be.visible');

    cy.get('[data-cy=media-folder]').contains(childName)
      .parents('[data-cy=media-folder]').find('[data-cy=btn-rename-folder]').click();
    cy.get('select[data-cy=folder-permission]').should('have.value', 'private');
    cy.get('.modal button.is-primary').click();
    cy.reload();
    cy.get('[data-cy=media-folder]').contains(rootName).click();
    cy.get('[data-cy=media-folder]').contains(childName)
      .parents('[data-cy=media-folder]').find('.tag').should('contain', '个人');

    cy.get('[data-cy=media-folder]').contains(childName)
      .parents('[data-cy=media-folder]')
      .find('[data-cy=btn-delete-folder]')
      .click();
    cy.get('.modal button.is-primary').click();
    cy.contains('[data-cy=media-folder]', childName).should('not.exist');

    cy.get('.media-breadcrumb a').first().click();
    cy.get('[data-cy=media-folder]').contains(rootName)
      .parents('[data-cy=media-folder]')
      .find('[data-cy=btn-delete-folder]')
      .click();
    cy.get('.modal button.is-primary').click();
    cy.contains('[data-cy=media-folder]', rootName).should('not.exist');
  });
});
