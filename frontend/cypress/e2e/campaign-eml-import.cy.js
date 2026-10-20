/* eslint-env mocha */
/* global cy, expect, Cypress */

const editorBody = () => cy.get('.tox-edit-area iframe').its('0.contentDocument.body').should('not.be.empty').then(cy.wrap);
const select = (raw, name = 'message.eml') => cy.get('[data-cy=eml-file]').selectFile({
  contents: Cypress.Buffer.from(raw), fileName: name, mimeType: 'message/rfc822',
}, { force: true });
const confirm = () => cy.get('.dialog .modal-card-foot button').contains(/^Ok$/i).click();
const plain = 'From: sender@example.test\r\nSubject: Import only body\r\nContent-Type: text/plain; charset=utf-8\r\n'
  + 'Content-Transfer-Encoding: quoted-printable\r\n\r\n=E4=BD=A0=E5=A5=BD\r\nPlain <text> & content';

const mixed = (win, html) => {
  const canvas = win.document.createElement('canvas');
  canvas.width = 80;
  canvas.height = 40;
  canvas.getContext('2d').fillRect(0, 0, 80, 40);
  const png = canvas.toDataURL().split(',')[1];
  return 'From: sender@example.test\r\nSubject: Do not replace subject\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary="outer"\r\n\r\n'
    + '--outer\r\nContent-Type: multipart/related; boundary="related"\r\n\r\n'
    + '--related\r\nContent-Type: multipart/alternative; boundary="alternative"\r\n\r\n'
    + '--alternative\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nFallback plain\r\n'
    + '--alternative\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n'
    + `${Cypress.Buffer.from(html).toString('base64')}\r\n--alternative--\r\n`
    + '--related\r\nContent-Type: image/png; name="first.png"\r\nContent-ID: <first@example>\r\nContent-Transfer-Encoding: base64\r\n\r\n'
    + `${png}\r\n`
    + '--related\r\nContent-Type: image/png; name="second.png"\r\nContent-ID: <second@example>\r\nContent-Transfer-Encoding: base64\r\n\r\n'
    + `${png}\r\n--related--\r\n`
    + '--outer\r\nContent-Type: application/pdf; name="not-imported.pdf"\r\nContent-Disposition: attachment; filename="not-imported.pdf"\r\n\r\nPDF placeholder\r\n'
    + '--outer--\r\n';
};

describe('Campaign EML import', () => {
  before(() => cy.resetDB());
  beforeEach(() => {
    cy.viewport(1648, 1244);
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/campaigns');
    cy.request('POST', '/api/campaigns', {
      name: 'EML import regression', subject: 'Existing subject', type: 'regular', content_type: 'richtext',
      body: '<p>Existing draft</p>', customer_list_ids: [1], smtp_source: 'personal', smtp_rate_limit: 20, template_id: 1,
    }).then(({ body }) => cy.visit(`/admin/campaigns/${body.data.id}#content`));
    cy.window().should((win) => expect(win.tinymce?.editors[0]?.initialized).to.eq(true));
  });

  it('imports nested MIME, preserves table layout and CID order, previews and persists uploaded images', () => {
    const uploaded = [];
    cy.intercept('POST', '/api/media', (req) => req.continue((res) => { uploaded.push(res.body.data.id); })).as('upload');
    cy.window().then((win) => select(mixed(win,
      '<html><body><p>你好 Before</p><table style="width:300px;border-collapse:collapse"><tr><td style="color:rgb(255,0,0)">'
      + '<img src="cid:first%40example" alt="first" width="80" height="40"></td></tr></table><p>Middle</p>'
      + '<img src="cid:second@example" alt="second"><p>After</p><img src="cid:first@example" alt="repeated"></body></html>')));
    editorBody().should('contain', 'Existing draft');
    confirm();
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.get('.toast').should('contain', '2 embedded images').and('contain', '1 other attachments');
    editorBody().should('contain', '你好 Before').and('contain', 'Middle').and('contain', 'After').and('not.contain', 'Fallback plain');
    editorBody().find('table').should('have.attr', 'style').and('contain', '300px');
    editorBody().find('img').should(($images) => {
      expect(Array.from($images).map((img) => img.naturalWidth)).to.deep.eq([80, 80, 80]);
      expect(Array.from($images).map((img) => img.alt)).to.deep.eq(['first', 'second', 'repeated']);
      expect($images[0].src).to.eq($images[2].src);
    });
    cy.get('[data-cy=btn-preview]').click();
    cy.get('#iframe').invoke('attr', 'srcdoc').should('contain', 'data:image/png;base64,').and('contain', '你好 Before');
    cy.get('.modal-card-foot button').click();
    cy.intercept('PUT', '/api/campaigns/*').as('save');
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@save').then(({ request, response }) => {
      expect(response.statusCode).to.eq(200);
      expect(request.body.subject).to.eq('Existing subject');
      expect(request.body.body).not.to.match(/cid:|data:image|blob:/);
      expect(request.body.media.slice().sort()).to.deep.eq(uploaded.sort());
    });
    cy.reload();
    editorBody().find('img').should(($images) => expect(Array.from($images).map((img) => img.naturalWidth)).to.deep.eq([80, 80, 80]));
  });

  it('imports quoted-printable UTF-8 plain text without HTML interpretation', () => {
    select(plain);
    confirm();
    cy.get('textarea[name=content]').should('have.value', '你好\nPlain <text> & content\n');
    cy.get('select[name=content_type]').should('have.value', 'plain');
    cy.intercept('PUT', '/api/campaigns/*').as('save');
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@save').its('response.statusCode').should('eq', 200);
    cy.reload();
    cy.get('textarea[name=content]').should('contain.value', '你好');
  });

  it('leaves the draft unchanged on cancellation, malformed EML and missing images', () => {
    select(plain);
    cy.get('.dialog .modal-card-foot button').contains('Cancel').click();
    editorBody().should('contain', 'Existing draft');
    select('Not a mail');
    confirm();
    cy.get('[data-cy=eml-error]').should('contain', 'Unable to parse');
    cy.window().then((win) => select(mixed(win, '<p>Broken</p><img src="cid:missing@example">')));
    confirm();
    cy.get('[data-cy=eml-error]').should('contain', 'missing or ambiguous');
    editorBody().should('contain', 'Existing draft').and('not.contain', 'Broken');
    cy.get('[data-cy=btn-import-eml]').should('not.be.disabled');
  });

  it('retains the original draft on upload failure and supports retrying the same file', () => {
    let fail = true;
    cy.intercept('POST', '/api/media', (req) => {
      if (fail) req.reply({ statusCode: 403, body: { message: 'Upload denied' } });
      else req.continue();
    }).as('upload');
    let raw;
    cy.window().then((win) => { raw = mixed(win, '<p>Imported</p><img src="cid:first@example">'); select(raw); });
    confirm();
    cy.wait('@upload').its('response.statusCode').should('eq', 403);
    cy.get('[data-cy=eml-error]').should('contain', 'Image upload failed');
    editorBody().should('contain', 'Existing draft').find('img').should('not.exist');
    cy.then(() => { fail = false; select(raw); });
    confirm();
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    editorBody().should('contain', 'Imported').find('img').should(($images) => expect($images[0].naturalWidth).to.eq(80));
  });

  it('sanitizes active HTML and CSS, imports filename references, and decodes legacy charsets', () => {
    cy.window().then((win) => select(mixed(win,
      '<style>@import "https://untrusted.example/style";</style><script>window.emlExecuted = true;</script>'
      + '<p style="color:red;position:fixed;background-image:url(javascript:alert(1))" onclick="alert(1)">Safe</p>'
      + '<iframe src="https://untrusted.example"></iframe><img src="Attach/first.png" onerror="alert(1)">'
      + '<a href="javascript:alert(1)">Link</a>')));
    confirm();
    cy.get('.toast').should('contain', 'Body imported');
    cy.window().then((win) => {
      const html = win.tinymce.editors[0].getContent();
      expect(html).not.to.match(/<script|<iframe|<style|onclick|onerror|javascript:|position:|@import/);
      expect(win.emlExecuted).to.eq(undefined);
    });
    select('Subject: Legacy charset\r\nContent-Type: text/plain; charset=gb18030\r\nContent-Transfer-Encoding: base64\r\n\r\nxOO6ww==');
    confirm();
    cy.get('textarea[name=content]').should('have.value', '你好');
  });

  it('blocks saving during upload and disables import on read-only campaigns', () => {
    cy.intercept('POST', '/api/media', (req) => req.continue((res) => res.setDelay(5000))).as('upload');
    let saves = 0;
    cy.intercept('PUT', '/api/campaigns/*', (req) => { saves += 1; req.continue(); });
    cy.window().then((win) => select(mixed(win, '<p>Slow</p><img src="cid:first@example">')));
    confirm();
    cy.get('[data-cy=btn-import-eml]').should('be.disabled');
    cy.get('.campaign > header [data-cy=btn-save]').click({ force: true });
    cy.wait('@upload');
    editorBody().should('contain', 'Slow');
    cy.then(() => expect(saves).to.eq(0));
    cy.location('pathname').then((path) => {
      cy.intercept('GET', `/api/campaigns/${path.split('/').pop()}`, (req) => req.continue((res) => { res.body.data.status = 'finished'; }));
    });
    cy.reload();
    cy.get('[data-cy=btn-import-eml]').should('be.disabled');
    cy.get('[data-cy=eml-file]').should('be.disabled');
  });

  it('rejects empty, oversized and non-EML files without changing the draft', () => {
    select('', 'empty.eml');
    confirm();
    cy.get('[data-cy=eml-error]').should('contain', '32 MiB');
    select(plain, 'unsupported.msg');
    confirm();
    cy.get('[data-cy=eml-error]').should('contain', 'not .msg');
    select(Cypress.Buffer.alloc(32 * 1024 * 1024 + 1), 'oversized.eml');
    confirm();
    cy.get('[data-cy=eml-error]').should('contain', '32 MiB');
    editorBody().should('contain', 'Existing draft');
  });

  it('keeps the new import and preview controls within a mobile viewport', () => {
    cy.viewport(390, 844);
    cy.get('[data-cy=btn-import-eml], [data-cy=btn-preview]').each(($button) => {
      const bounds = $button[0].getBoundingClientRect();
      expect(bounds.left).to.be.at.least(0);
      expect(bounds.right).to.be.at.most(390);
    });
  });
});
