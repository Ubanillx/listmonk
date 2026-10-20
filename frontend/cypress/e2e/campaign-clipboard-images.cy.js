/* eslint-env mocha */
/* global cy, expect, Cypress */

const editorBody = () => cy.get('.tox-edit-area iframe').its('0.contentDocument.body').should('not.be.empty').then(cy.wrap);

const image = (win, width = 80) => {
  const canvas = win.document.createElement('canvas');
  canvas.width = width;
  canvas.height = 40;
  canvas.getContext('2d').fillRect(0, 0, width, 40);
  const dataURL = canvas.toDataURL('image/png');
  return { dataURL, bytes: Cypress.Buffer.from(dataURL.split(',')[1], 'base64') };
};

const paste = (win, html, files = [], rtf = '', text = 'Plain fallback') => {
  const editor = win.tinymce.editors[0];
  editor.focus();
  const clipboard = new win.DataTransfer();
  if (html) clipboard.setData('text/html', html);
  if (text) clipboard.setData('text/plain', text);
  if (rtf) clipboard.setData('text/rtf', rtf);
  files.forEach((file) => clipboard.items.add(file));
  editor.getBody().dispatchEvent(new win.ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
};

const assertOrder = () => editorBody().should(($body) => {
  const content = Array.from(($body[0] || $body).querySelectorAll('p, img')).map((node) => (node.tagName === 'IMG' ? node.alt : node.textContent)).filter(Boolean);
  expect(content).to.deep.eq(['Before', 'first', 'Middle', 'second', 'After']);
});

describe('Campaign clipboard images', () => {
  before(() => cy.resetDB());
  beforeEach(() => {
    cy.viewport(1644, 1244);
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/campaigns');
    cy.request('POST', '/api/campaigns', {
      name: 'Clipboard image regression',
      subject: 'Clipboard image regression',
      type: 'regular',
      content_type: 'richtext',
      body: '<p></p>',
      customer_list_ids: [1],
      smtp_source: 'personal',
      smtp_rate_limit: 20,
      template_id: 1,
    }).then(({ body }) => cy.visit(`/admin/campaigns/${body.data.id}#content`));
    cy.get('.tox-tinymce').should('be.visible');
    cy.window().should((win) => expect(win.tinymce.editors[0].initialized).to.eq(true));
  });

  it('keeps mixed HTML and clipboard files in order, automatically uploads and persists them', () => {
    const uploaded = [];
    cy.intercept('POST', '/api/media', (req) => req.continue((res) => {
      uploaded.push(res.body.data);
      res.setDelay(500);
    })).as('upload');
    cy.window().then((win) => {
      const first = image(win);
      const second = image(win, 120);
      paste(
        win,
        `<p>Before</p><p><img src="${first.dataURL}" alt="first"></p><p>Middle</p>`
        + '<p><img src="file:///C:/paste/second.png" alt="second"></p><p>After</p>',
        [new win.File([second.bytes], 'second.png', { type: 'image/png' })],
      );
    });
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    assertOrder();
    editorBody().find('img').should(($images) => {
      expect(Array.from($images).map((i) => i.naturalWidth)).to.deep.eq([80, 120]);
      Array.from($images).forEach((i) => expect(i.src).to.contain('/api/media/file/'));
    });
    cy.intercept('PUT', '/api/campaigns/*').as('save');
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@save').then(({ request, response }) => {
      expect(response.statusCode).to.eq(200);
      expect(request.body.body).not.to.match(/(?:data:image|blob:|file:|LMCLIPIMAGE)/);
      expect(request.body.media.slice().sort()).to.deep.eq(uploaded.map((m) => m.id).sort());
      expect(response.body.data.media.map((m) => m.id).sort()).to.deep.eq(uploaded.map((m) => m.id).sort());
    });
    cy.reload();
    assertOrder();
    cy.request('/api/media').then(({ body }) => {
      const ids = body.data.results.map((m) => m.id);
      uploaded.forEach((m) => expect(ids).to.include(m.id));
    });
  });

  it('maps Office relative Attach image references to clipboard files', () => {
    cy.intercept('POST', '/api/media').as('upload');
    cy.window().then((win) => {
      const files = [80, 120, 160, 200].map((width, index) => {
        const item = image(win, width);
        return new win.File([item.bytes], `image00${index + 5}(10-20-11-50-52).png`, { type: 'image/png' });
      });
      const html = files.map((file, index) => `<p>Text ${index + 1}</p><p><img src="Attach/${file.name}" /></p>`).join('');
      paste(win, html, files);
    });
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    editorBody().find('img').should(($images) => {
      expect(Array.from($images).map((i) => i.naturalWidth)).to.deep.eq([80, 120, 160, 200]);
      Array.from($images).forEach((i) => expect(i.src).to.contain('/api/media/file/'));
    });
  });

  it('extracts Word/WPS RTF pictures without losing their positions or a quick save', () => {
    cy.intercept('POST', '/api/media').as('upload');
    cy.intercept('PUT', '/api/campaigns/*').as('save');
    cy.window().then((win) => {
      const first = image(win);
      const second = image(win, 120);
      const html = '<p class="MsoNormal">Before</p><p class="MsoNormal"><img src="file:///word/image001.png" alt="first"></p>'
        + '<p class="MsoNormal">Middle</p><p class="MsoNormal"><img src="file:///word/image002.png" alt="second"></p><p class="MsoNormal">After</p>';
      const rtf = `{\\rtf1{\\pict\\pngblip\\picw80\\pich40{\\*\\blipuid ABCD} ${first.bytes.toString('hex')}}`
        + `{\\pict\\pngblip\\picw120\\pich40 ${second.bytes.toString('hex')}}}`;
      paste(win, html, [], rtf);
    });
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.wait('@save').its('request.body.body').should('contain', 'Before').and('not.contain', 'file:');
    cy.reload();
    assertOrder();
  });

  it('imports web images when readable and blocks saving if a pasted image has no readable bytes', () => {
    cy.window().then((win) => {
      const { bytes } = image(win);
      cy.intercept('GET', 'https://clipboard.example.test/readable.png', {
        statusCode: 200, headers: { 'content-type': 'image/png', 'access-control-allow-origin': '*' }, body: bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength),
      }).as('download');
    });
    cy.intercept('POST', '/api/media').as('upload');
    cy.window().then((win) => paste(win, '<p>Web image</p><img src="https://clipboard.example.test/readable.png" alt="web">'));
    cy.wait('@download');
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    editorBody().find('img').should(($images) => expect($images[0].naturalWidth).to.eq(80));
    let saves = 0;
    cy.intercept('PUT', '/api/campaigns/*', (req) => { saves += 1; req.continue(); });
    cy.window().then((win) => paste(win, '<p>Missing image</p><img src="file:///unavailable.png" alt="missing">'));
    cy.get('.toast').should('contain', 'Some pasted images could not be imported');
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.get('.toast').should('contain', 'The image could not be uploaded');
    cy.then(() => expect(saves).to.eq(0));
    editorBody().should('contain', 'Missing image');
  });

  it('preserves text plus a file and retains uploaded images through undo and redo', () => {
    cy.intercept('POST', '/api/media').as('upload');
    cy.window().then((win) => paste(win, '', [new win.File([image(win).bytes], 'plain.png', { type: 'image/png' })], '', 'Text with image'));
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    editorBody().should('contain', 'Text with image').find('img').should('have.length', 1);
    cy.get('button[title="Undo"]').click();
    editorBody().find('img').should('not.exist');
    cy.get('button[title="Redo"]').click();
    editorBody().find('img').should(($images) => {
      expect($images[0].src).to.contain('/api/media/file/');
      expect($images[0].naturalWidth).to.eq(80);
    });
  });

  it('keeps the text and image on upload failure, then retries and saves successfully', () => {
    let fail = true;
    cy.intercept('POST', '/api/media', (req) => {
      if (fail) req.reply({ statusCode: 403, body: { message: 'Clipboard upload denied' } });
      else req.continue();
    }).as('upload');
    let saves = 0;
    cy.intercept('PUT', '/api/campaigns/*', (req) => { saves += 1; req.continue(); }).as('save');
    cy.window().then((win) => paste(win, `<p>Keep this text</p><img src="${image(win).dataURL}" alt="retry">`));
    cy.wait('@upload').its('response.statusCode').should('eq', 403);
    editorBody().should('contain', 'Keep this text').find('img').should('have.length', 1);
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@upload').its('response.statusCode').should('eq', 403);
    cy.get('.toast').should('contain', 'The image could not be uploaded');
    cy.then(() => { expect(saves).to.eq(0); fail = false; });
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.wait('@save').its('response.body.data.media').should('have.length', 1);
    cy.reload();
    editorBody().should('contain', 'Keep this text').find('img').should(($images) => expect($images[0].naturalWidth).to.eq(80));
  });

  it('associates copied library images and respects plain text paste mode', () => {
    cy.intercept('POST', '/api/media').as('upload');
    let source;
    cy.window().then((win) => paste(win, `<p>Original</p><img src="${image(win).dataURL}" alt="original">`));
    cy.wait('@upload').then(({ response }) => { source = response.body.data.url; });
    editorBody().find('img').should(($images) => expect($images[0].src).to.contain('/api/media/file/'));
    cy.window().then((win) => {
      const editor = win.tinymce.editors[0];
      editor.setContent('<p></p>');
      paste(win, `<p>Library copy</p><img src="${source}" alt="copy">`);
    });
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    editorBody().find('img').should(($images) => expect($images[0].naturalWidth).to.eq(80));
    cy.window().then((win) => {
      const editor = win.tinymce.editors[0];
      editor.setContent('<p></p>');
      editor.execCommand('mceTogglePlainTextPaste');
      paste(win, `<p>Text only</p><img src="${image(win).dataURL}">`, [], '', 'Text only');
    });
    editorBody().should('contain', 'Text only').find('img').should('not.exist');
  });
});
