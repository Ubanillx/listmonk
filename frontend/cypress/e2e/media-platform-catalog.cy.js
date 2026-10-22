/* eslint-env mocha */
/* global cy, expect */

describe('Platform media catalog', () => {
  it('opens the full-platform organization and owner browser for administrators', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.intercept('GET', '/api/media/categories*', {
      data: [
        { organization_id: 0, organization_name: '', owner_user_id: 12, owner_name: 'Alice', media_count: 2 },
        { organization_id: 34, organization_name: 'Studio', owner_user_id: 56, owner_name: 'Bob', media_count: 1 },
      ],
    }).as('categories');
    cy.intercept({ method: 'GET', pathname: '/api/media', query: { scope: 'all_owners' } }, (req) => {
      const selected = req.query.organization_id === '34' && req.query.owner_user_id === '56';
      req.reply({ data: {
        results: selected ? [{
          id: 104, filename: 'studio.png', url: '/api/media/file/104/studio.png?scope=all_owners',
          organization_name: 'Studio', owner_name: 'Bob', folder_name: 'Campaign art',
          created_at: '2026-10-22T00:00:00Z',
        }] : [],
        total: selected ? 1 : 0, page: 1, per_page: 20,
      } });
    }).as('catalog');
    cy.loginAndVisit('/admin/campaigns/media');
    cy.wait('@categories');
    cy.wait('@catalog').then(({ request }) => {
      expect(request.query).to.include({ scope: 'all_owners' });
      expect(request.query).not.to.have.property('organization_id');
    });
    cy.get('[data-cy=media-organizations]').should('contain', 'Studio');
    cy.get('[data-cy=media-owners]').should('contain', 'Bob');
    cy.get('[data-cy=btn-toggle-upload]').should('not.exist');
    cy.get('[data-cy=media-current-scope], [data-cy=media-all-scope]').should('not.exist');
    cy.contains('[data-cy=media-organizations] button', 'Studio').click();
    cy.wait('@catalog').its('request.query').should('include', { scope: 'all_owners', organization_id: '34' });
    cy.get('[data-cy=media-owners]').should('contain', 'Bob').and('not.contain', 'Alice');
    cy.contains('[data-cy=media-owners] button', 'Bob').click();
    cy.wait('@catalog').then(({ request }) => {
      expect(request.query).to.include({ scope: 'all_owners', organization_id: '34', owner_user_id: '56' });
    });
    cy.get('[data-cy=media-catalog-context]').should('contain', 'Studio').and('contain', 'Bob');
    cy.contains('.media-files .item', 'studio.png').should('contain', 'Studio').and('contain', 'Campaign art');
    cy.get('.media-files .item .thumb-link').should('have.attr', 'href', '/api/media/file/104/studio.png?scope=all_owners');
    cy.get('.pagination-wrapper').should('have.length', 1);
  });

  it('opens only the editable personal library for an ordinary user', () => {
    cy.resetDB();
    cy.loginAndVisit('/admin/campaigns/media');
    cy.request('POST', '/api/roles/users', {
      name: 'Media member', permissions: ['media:get', 'media:manage', 'workspaces:personal'],
    }).then(({ body }) => cy.request('POST', '/api/users', {
      username: 'media-member', name: 'Media member', email: 'media-member@example.com',
      type: 'user', status: 'enabled', password_login: true, password: 'media-member-test', user_role_id: body.data.id,
    }));
    cy.clearCookies();
    cy.intercept({ method: 'GET', pathname: '/api/media' }).as('workspaceMedia');
    cy.request('/admin/login').then(({ body }) => cy.request({
      method: 'POST', url: '/admin/login', form: true,
      body: {
        username: 'media-member', password: 'media-member-test',
        nonce: /name="nonce" value="([^"]+)"/.exec(body)[1], next: '/admin/campaigns/media',
      },
    }));
    cy.visit('/admin/campaigns/media');
    cy.wait('@workspaceMedia').then(({ request }) => {
      expect(request.query).not.to.have.property('scope');
      expect(request.query.folder_id).to.equal('0');
    });
    cy.get('[data-cy=media-organizations], [data-cy=media-owners]').should('not.exist');
    cy.get('[data-cy=btn-toggle-upload]').should('exist');
    cy.get('form[data-cy=upload]').should('not.exist');
    cy.get('[data-cy=btn-toggle-upload]').click();
    cy.get('form[data-cy=upload]').should('be.visible');
    cy.reload();
    cy.get('form[data-cy=upload]').should('not.exist');
  });
});
