import DOMPurify from 'dompurify';

const MAX_BYTES = 32 * 1024 * 1024;
const IMAGE_TYPES = /^image\/(?:png|jpeg|gif|webp|bmp|avif)$/i;
const SAFE_STYLE = new RegExp('^(?:color|background-color|font(?:-family|-size|-style|-weight)?|line-height|letter-spacing|text-align|text-decoration|'
  + 'vertical-align|white-space|width|height|max-width|min-width|border(?:-(?:top|right|bottom|left|width|style|color|collapse|spacing))?|'
  + 'padding(?:-(?:top|right|bottom|left))?|margin(?:-(?:top|right|bottom|left))?)$');
const sanitizerOptions = {
  USE_PROFILES: { html: true },
  FORBID_TAGS: ['style', 'form', 'input', 'button', 'textarea', 'select', 'video', 'audio', 'source'],
  FORBID_ATTR: ['srcset', 'id', 'name'],
  ALLOW_DATA_ATTR: false,
};

export const parseEml = async (file) => {
  if (!/\.(?:eml|msg)$/i.test(file.name)) throw new Error('invalid');
  if (!file.size || file.size > MAX_BYTES) throw new Error('size');
  const bytes = await file.arrayBuffer();
  const isMsg = /\.msg$/i.test(file.name);
  // Reject arbitrary text files; an EML must have a header block before its body.
  const header = new TextDecoder().decode(bytes.slice(0, 256 * 1024)).split(/\r?\n\r?\n/)[0];
  if (!isMsg && !/^(?:from|subject|mime-version|content-type|date|to):/im.test(header)) throw new Error('invalid');
  return new Promise((resolve, reject) => {
    const worker = isMsg
      ? new Worker(new URL('./msgParser.worker.js', import.meta.url), { type: 'module' })
      : new Worker(new URL('./emlParser.worker.js', import.meta.url), { type: 'module' });
    let timer;
    const finish = (error, result) => {
      clearTimeout(timer);
      worker.terminate();
      if (error) reject(new Error(error));
      else resolve(result);
    };
    timer = setTimeout(() => finish('invalid'), 30000);
    worker.onmessage = ({ data }) => finish(data.error, data);
    worker.onerror = () => finish('invalid');
    worker.postMessage(bytes, [bytes]);
  });
};

const decode = (value) => {
  try { return decodeURIComponent(value); } catch (error) { return value; }
};
const basename = (value) => decode(value || '').split(/[\\/]/).pop();
const contentID = (value) => decode(value || '').replace(/^cid:/i, '').replace(/^<|>$/g, '');

export const prepareEml = (email) => {
  if (!email.html && !email.text) throw new Error('empty');
  if (!email.html) {
    return {
      body: email.text, contentType: 'plain', images: [], ignored: (email.attachments || []).length,
    };
  }
  const container = document.createElement('template');
  container.innerHTML = DOMPurify.sanitize(email.html, sanitizerOptions);
  const attachments = email.attachments || [];
  const images = new Map();
  const references = [];
  const resolveImage = (source, update) => {
    const src = source.trim();
    if (/^https?:\/\//i.test(src)) return;
    let attachment;
    if (/^data:image\/(?:png|jpeg|gif|webp|bmp|avif);base64,/i.test(src)) {
      if (!images.has(src)) {
        const [prefix, data] = src.split(',');
        let binary;
        try { binary = atob(data); } catch (error) { throw new Error('invalid'); }
        attachment = { mimeType: prefix.slice(5).split(';')[0], content: Uint8Array.from(binary, (c) => c.charCodeAt(0)), filename: 'embedded-image' };
        images.set(src, attachment);
      }
      attachment = images.get(src);
    } else {
      const matches = attachments.filter((item) => (/^cid:/i.test(src)
        ? item.contentId && contentID(item.contentId) === contentID(src)
        : !/^[a-z][a-z\d+.-]*:/i.test(src) && item.filename && basename(item.filename) === basename(src)));
      if (matches.length !== 1) throw new Error('missing');
      [attachment] = matches;
      if (!IMAGE_TYPES.test(attachment.mimeType)) throw new Error('unsupported');
      images.set(attachment, attachment);
    }
    references.push({ attachment, update });
  };
  container.content.querySelectorAll('*').forEach((node) => {
    if (node.tagName === 'IMG') {
      if (!node.getAttribute('src')) throw new Error('missing');
      resolveImage(node.getAttribute('src'), (url) => node.setAttribute('src', url));
    }
    if (node.hasAttribute('background')) resolveImage(node.getAttribute('background'), (url) => node.setAttribute('background', url));
    // Keep email layout styles, but never import active CSS or stylesheet rules.
    const background = node.style.backgroundImage;
    const { style } = node;
    Array.from(node.style).forEach((property) => {
      if (!SAFE_STYLE.test(property) || /url\s*\(|expression|\\|@/i.test(node.style.getPropertyValue(property))) node.style.removeProperty(property);
    });
    if (background) {
      const match = /^url\(["']?([^"'()]+)["']?\)$/.exec(background);
      if (match) {
        const source = match[1];
        if (/^https?:\/\//i.test(source)) style.backgroundImage = background;
        else resolveImage(source, (url) => { style.backgroundImage = `url("${url}")`; });
      }
    }
  });
  const unique = [...new Set(images.values())];
  if (unique.reduce((sum, item) => sum + item.content.byteLength, 0) > MAX_BYTES) throw new Error('size');
  return {
    contentType: 'richtext',
    images: unique,
    ignored: attachments.filter((item) => !unique.includes(item)).length,
    render(urls) {
      references.forEach(({ attachment, update }) => update(urls.get(attachment)));
      return DOMPurify.sanitize(container.innerHTML, sanitizerOptions);
    },
  };
};

export const importEml = async (file, upload) => {
  const prepared = prepareEml(await parseEml(file));
  const urls = new Map();
  const media = [];
  // Validate every reference before uploading anything; apply the body only after all uploads succeed.
  for (let index = 0; index < prepared.images.length; index += 1) {
    const image = prepared.images[index];
    const extension = image.mimeType.split('/')[1].replace('jpeg', 'jpg');
    const data = new FormData();
    data.set('file', new Blob([image.content], { type: image.mimeType }), `eml-image-${index + 1}.${extension}`);
    // eslint-disable-next-line no-await-in-loop
    const uploaded = await upload(data);
    if (!uploaded?.url) throw new Error('upload');
    urls.set(image, uploaded.url);
    media.push(uploaded);
  }
  return {
    body: prepared.render ? prepared.render(urls) : prepared.body, contentType: prepared.contentType, media, ignored: prepared.ignored,
  };
};
