/* eslint-env mocha */
/* global cy, expect */

const email = '{{ .Customer.Email }}';
const name = '{{ .Customer.Name }}';
const chip = (text) => cy.contains('[data-cy=insert-placeholder]', text);
const code = () => cy.get('.editor .cm-content');
const richBody = () => cy.get('.tox-edit-area iframe').its('0.contentDocument.body').should('not.be.empty').then(cy.wrap);
const visualBody = () => cy.get('#visual-editor').its('0.contentDocument.body').should('not.be.empty').then(cy.wrap);
const moveToMiddle = '{ctrl+home}{rightArrow}{rightArrow}{rightArrow}{rightArrow}{rightArrow}';

const openCampaign = (type, extra = {}) => cy.request('POST', '/api/campaigns', {
  name: `Placeholder insertion ${type}`,
  subject: 'Placeholder insertion',
  type: 'regular',
  content_type: type,
  body: 'LEFT RIGHT',
  customer_list_ids: [1],
  smtp_source: 'personal',
  smtp_rate_limit: 20,
  template_id: type === 'visual' ? null : 1,
  ...extra,
}).then(({ body }) => {
  cy.wrap(body.data.id).as('campaignId');
  cy.visit(`/admin/campaigns/${body.data.id}#content`);
  chip(email).should('be.visible');
});

const save = () => {
  cy.intercept('PUT', '/api/campaigns/*').as('save');
  cy.get('.campaign > header [data-cy=btn-save]').click();
  return cy.wait('@save').then((result) => {
    expect(result.response.statusCode).to.eq(200);
    return result.request.body;
  });
};

describe('Campaign placeholder insertion', () => {
  before(() => cy.resetDB());

  beforeEach(() => {
    cy.viewport(1644, 1244);
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/campaigns');
  });

  ['html', 'markdown'].forEach((type) => {
    it(`inserts repeatedly at the ${type} caret, replaces selection, undoes and persists`, () => {
      openCampaign(type);
      code().type(moveToMiddle);
      chip(email).click();
      code().should('have.text', `LEFT ${email}RIGHT`).and('have.focus');
      chip(name).click();
      code().should('have.text', `LEFT ${email}${name}RIGHT`);
      code().type('{ctrl+z}');
      code().should('have.text', `LEFT ${email}RIGHT`);
      code().type('{ctrl+y}');
      code().should('have.text', `LEFT ${email}${name}RIGHT`);
      code().type('{ctrl+end}{shift+leftArrow}{shift+leftArrow}{shift+leftArrow}{shift+leftArrow}{shift+leftArrow}');
      // Keyboard activation moves focus out of the editor before inserting.
      chip(email).focus().type('{enter}');
      const expected = `LEFT ${email}${name}${email}`;
      code().should('have.text', expected).and('have.focus');
      save().its('body').should('eq', expected);
      cy.reload();
      code().should('have.text', expected);
    });
  });

  it('preserves rich text formatting and selection through mouse and keyboard activation', () => {
    openCampaign('richtext', { body: '<p>LEFT <strong>RIGHT</strong></p>' });
    richBody().find('strong').should('have.text', 'RIGHT');
    cy.window().then((win) => {
      const editor = win.tinymce.editors[0];
      editor.focus();
      editor.selection.setCursorLocation(editor.getBody().querySelector('p').firstChild, 5);
    });
    chip(email).click();
    richBody().find('p').should('have.text', `LEFT ${email}RIGHT`);
    chip(name).click();
    richBody().find('p').should('have.text', `LEFT ${email}${name}RIGHT`);
    cy.get('button[title=Undo]').click();
    richBody().find('p').should('have.text', `LEFT ${email}RIGHT`);
    cy.get('button[title=Redo]').click();
    cy.window().then((win) => {
      const editor = win.tinymce.editors[0];
      const text = editor.getBody().querySelector('strong').firstChild;
      const range = editor.getDoc().createRange();
      range.setStart(text, 0);
      range.setEnd(text, text.length);
      editor.focus();
      editor.selection.setRng(range);
      editor.nodeChanged();
    });
    chip(email).focus().type('{enter}');
    richBody().find('strong').should('have.text', email);
    save().its('body').should('contain', `LEFT ${email}${name}`).and('contain', `<strong>${email}</strong>`);
    cy.reload();
    richBody().find('p').should('have.text', `LEFT ${email}${name}${email}`);
    richBody().find('strong').should('have.text', email);
  });

  it('inserts in plain text with native undo and keyboard focus restoration', () => {
    openCampaign('plain');
    cy.get('textarea[name=content]').type(moveToMiddle);
    chip(email).click();
    cy.get('textarea[name=content]').should('have.value', `LEFT ${email}RIGHT`).and('have.focus');
    // Cypress synthetic Ctrl+Z does not invoke a textarea's native undo action.
    cy.document().then((doc) => expect(doc.execCommand('undo')).to.eq(true));
    cy.get('textarea[name=content]').should('have.value', 'LEFT RIGHT');
    chip(name).focus().type('{enter}');
    cy.get('textarea[name=content]').should('have.value', `LEFT ${name}RIGHT`).and('have.focus');
    save().its('body').should('eq', `LEFT ${name}RIGHT`);
    cy.reload();
    cy.get('textarea[name=content]').should('have.value', `LEFT ${name}RIGHT`);
  });

  it('targets the focused alternate text and returns to the main rich text editor', () => {
    openCampaign('richtext', { body: '<p>LEFT RIGHT</p>' });
    cy.contains('a', 'Add alternate plain text message').click();
    cy.get('.alt-body textarea').clear().type('ALT END').type(moveToMiddle);
    chip(email).click();
    cy.get('.alt-body textarea').should('have.value', `ALT E${email}ND`).and('have.focus');
    richBody().click().type('{ctrl+end}');
    chip(name).click();
    richBody().find('p').should('have.text', `LEFT RIGHT${name}`);
    cy.get('.alt-body textarea').should('have.value', `ALT E${email}ND`);
    save().then((body) => {
      expect(body.altbody).to.eq(`ALT E${email}ND`);
      expect(body.body).to.contain(`LEFT RIGHT${name}`);
    });
  });

  it('inserts in the active visual editor text field and persists both source and HTML', () => {
    const source = {
      root: { type: 'EmailLayout', data: { childrenIds: ['text'] } },
      text: { type: 'Text', data: { props: { text: 'LEFT RIGHT' } } },
    };
    openCampaign('visual', { body_source: JSON.stringify(source) });
    visualBody().contains('LEFT RIGHT').should('be.visible');
    chip(email).click();
    cy.contains('Place the cursor in a text field in the editor first.').should('be.visible');
    visualBody().contains('LEFT RIGHT').click();
    visualBody().find('textarea:not([aria-hidden=true])').first().type(moveToMiddle);
    chip(email).click();
    visualBody().find('textarea:not([aria-hidden=true])').first().should('have.value', `LEFT ${email}RIGHT`);
    chip(name).click();
    visualBody().find('textarea:not([aria-hidden=true])').first().should('have.value', `LEFT ${email}${name}RIGHT`);
    save().then((body) => {
      expect(body.body).to.contain(`LEFT ${email}${name}RIGHT`);
      expect(JSON.parse(body.body_source).text.data.props.text).to.eq(`LEFT ${email}${name}RIGHT`);
    });
    cy.reload();
    visualBody().contains(`LEFT ${email}${name}RIGHT`).should('be.visible');
  });

  it('disables insertions for a read-only campaign', () => {
    cy.request('POST', '/api/campaigns', {
      name: 'Placeholder read-only',
      subject: 'Placeholder read-only',
      type: 'regular',
      content_type: 'html',
      body: 'LEFT RIGHT',
      customer_list_ids: [1],
      smtp_source: 'personal',
      smtp_rate_limit: 20,
      template_id: 1,
    }).then(({ body }) => {
      cy.intercept('GET', `/api/campaigns/${body.data.id}`, (req) => req.continue((res) => {
        res.body.data.status = 'finished';
      }));
      cy.visit(`/admin/campaigns/${body.data.id}#content`);
    });
    cy.get('[data-cy=insert-placeholder]').each(($button) => cy.wrap($button).should('be.disabled'));
    code().should('have.text', 'LEFT RIGHT');
    cy.get('.campaign > header [data-cy=btn-save]').should('not.exist');
  });
});
