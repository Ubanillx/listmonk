/* eslint-env mocha */
/* global cy, expect, Cypress */

const editorBody = () => cy.get('.tox-edit-area iframe').its('0.contentDocument.body').should('not.be.empty').then(cy.wrap);

const imageData = (win) => {
  const canvas = win.document.createElement('canvas');
  canvas.width = 320;
  canvas.height = 160;
  canvas.getContext('2d').fillRect(0, 0, 320, 160);
  return canvas.toDataURL('image/png');
};

describe('Campaign image editing', () => {
  before(() => {
    cy.resetDB();
  });

  beforeEach(() => {
    cy.viewport(1644, 1244);
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/campaigns');
    cy.request('POST', '/api/campaigns', {
      name: 'Image editing regression',
      subject: 'Image editing regression',
      type: 'regular',
      content_type: 'richtext',
      body: '<p>Image editing regression</p>',
      customer_list_ids: [1],
      smtp_source: 'personal',
      smtp_rate_limit: 20,
      template_id: 1,
    }).then(({ body }) => cy.visit(`/admin/campaigns/${body.data.id}#content`));
    cy.get('.tox-tinymce').should('be.visible');
  });

  it('opens new images on Upload and keeps existing image properties available', () => {
    cy.get('button[title="Insert/edit image"]').click();
    cy.contains('.tox-dialog [role=tab]', 'Upload').should('have.attr', 'aria-selected', 'true');
    cy.intercept('POST', '/api/media').as('upload');
    cy.window().then((win) => {
      cy.get('.tox-dialog input[type=file]').selectFile({
        contents: Cypress.Buffer.from(imageData(win).split(',')[1], 'base64'),
        fileName: 'image-editing.png',
        mimeType: 'image/png',
      }, { force: true });
    });
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.contains('.tox-dialog [role=tab]', 'General').should('have.attr', 'aria-selected', 'true');
    cy.contains('.tox-dialog button', 'Save').click();
    editorBody().find('img').should('be.visible').click();
    cy.get('button[title="Insert/edit image"]').click();
    cy.contains('.tox-dialog [role=tab]', 'General').should('have.attr', 'aria-selected', 'true');
    cy.contains('.tox-dialog label', 'Width').parent().find('input').should('have.value', '320');
    cy.contains('.tox-dialog button', 'Cancel').click();
  });

  it('shows usable corner handles, resizes proportionally, supports undo and persists dimensions', () => {
    cy.intercept('POST', '/api/media').as('upload');
    cy.window().then((win) => {
      win.tinymce.editors[0].setContent(`<p><img src="${imageData(win)}" width="320" height="160" alt="resize-example"></p>`);
    });
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.intercept('PUT', '/api/campaigns/*').as('save');
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@save').its('response.statusCode').should('eq', 200);
    cy.reload();
    editorBody().find('img').should(($image) => expect($image[0].naturalWidth).to.eq(320));
    editorBody().find('img').click();
    editorBody().find('.mce-resizehandle').should('have.length', 4).each(($handle) => {
      const rect = $handle[0].getBoundingClientRect();
      expect(rect.width, 'handle width').to.be.at.least(8);
      expect(rect.height, 'handle height').to.be.at.least(8);
    });
    editorBody().find('#mceResizeHandlese').then(($handle) => {
      const rect = $handle[0].getBoundingClientRect();
      const point = {
        screenX: rect.x + 5, screenY: rect.y + 5, clientX: rect.x + 5, clientY: rect.y + 5,
      };
      cy.wrap($handle).trigger('mousedown', { ...point, button: 0, buttons: 1 });
      editorBody().trigger('mousemove', {
        ...point, screenX: point.screenX + 100, screenY: point.screenY + 15, buttons: 1,
      });
      editorBody().trigger('mouseup', {
        ...point, screenX: point.screenX + 100, screenY: point.screenY + 15, button: 0,
      });
    });
    editorBody().find('img').should('have.attr', 'width', '420').and('have.attr', 'height', '210');
    cy.get('button[title="Undo"]').click();
    editorBody().find('img').should('have.attr', 'width', '320').and('have.attr', 'height', '160');
    cy.get('button[title="Redo"]').click();
    editorBody().find('img').should('have.attr', 'width', '420').and('have.attr', 'height', '210');
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@save').then(({ request, response }) => {
      expect(response.statusCode).to.eq(200);
      expect(request.body.body).to.contain('width="420"').and.to.contain('height="210"');
      expect(request.body.body).not.to.contain('mce-resizehandle');
    });
    cy.reload();
    editorBody().find('img').should('have.attr', 'width', '420').and('have.attr', 'height', '210');
  });
});
